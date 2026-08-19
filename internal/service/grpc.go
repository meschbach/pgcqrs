package service

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	storage2 "github.com/meschbach/pgcqrs/internal/service/storage"
	svviews "github.com/meschbach/pgcqrs/internal/service/views"
	"github.com/meschbach/pgcqrs/pkg/indexer/views"
	vgrpc "github.com/meschbach/pgcqrs/pkg/indexer/views/grpc"
	"github.com/meschbach/pgcqrs/pkg/ipc"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/thejerf/suture/v4"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type grpcCommand struct {
	ipc.UnimplementedCommandServer
	oldCore       *storage
	core          *storage2.Repository
	bus           *bus
	consumerStore *storage2.ConsumerStore
}

func (g *grpcCommand) CreateStream(ctx context.Context, in *ipc.CreateStreamIn) (*ipc.CreateStreamOut, error) {
	err := g.oldCore.ensureStream(ctx, in.Target.Domain, in.Target.Stream)
	return &ipc.CreateStreamOut{}, err
}

func (g *grpcCommand) Submit(ctx context.Context, in *ipc.SubmitIn) (*ipc.SubmitOut, error) {
	if in.Lock != nil {
		return g.submitWithLock(ctx, in)
	}

	id, err := g.oldCore.unsafeStore(ctx, in.Events.Domain, in.Events.Stream, in.Kind, in.Body)
	if err != nil {
		return nil, err
	}
	g.bus.dispatchOnEventStored(ctx, in.Events.Domain, in.Events.Stream, id, in.Kind, in.Body)
	return &ipc.SubmitOut{
		Id:    id,
		State: &ipc.Consistency{After: id},
	}, nil
}

func (g *grpcCommand) submitWithLock(ctx context.Context, in *ipc.SubmitIn) (ret *ipc.SubmitOut, retErr error) {
	tx, err := g.oldCore.pg.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, tx.Rollback(ctx))
		}
	}()

	return g.submitWithinTx(ctx, tx, in)
}

func (g *grpcCommand) submitWithinTx(ctx context.Context, tx pgx.Tx, in *ipc.SubmitIn) (*ipc.SubmitOut, error) {
	attrSet := attribute.NewSet(
		attribute.String("consumer-lock.domain", in.Events.Domain),
		attribute.String("consumer-lock.stream", in.Events.Stream),
		attribute.String("consumer-lock.consumer", in.Lock.Consumer),
	)
	storage2.AssertionChecks.Add(ctx, 1, metric.WithAttributeSet(attrSet))
	lock := &v1.Lock{Consumer: in.Lock.Consumer, Holder: in.Lock.Holder}
	if err := g.consumerStore.ResolveAndCheckLock(ctx, tx, in.Events.Domain, in.Events.Stream, lock); err != nil {
		storage2.AssertionRejections.Add(ctx, 1, metric.WithAttributeSet(attrSet))
		return nil, err
	}

	id, err := g.oldCore.unsafeStoreWith(ctx, tx, in.Events.Domain, in.Events.Stream, in.Kind, in.Body)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	g.bus.dispatchOnEventStored(ctx, in.Events.Domain, in.Events.Stream, id, in.Kind, in.Body)
	return &ipc.SubmitOut{
		Id:    id,
		State: &ipc.Consistency{After: id},
	}, nil
}

type grpcQuery struct {
	ipc.UnimplementedQueryServer
	oldCore *storage
	core    *storage2.Repository
	bus     *bus
}

func (g *grpcQuery) ListStreams(ctx context.Context, _ *ipc.ListStreamsIn) (*ipc.ListStreamsOut, error) {
	span := trace.SpanFromContext(ctx)
	//todo: convert to streaming to reduce heap usage on the service
	var out = &ipc.ListStreamsOut{}
	//todo: find common factors with s.v1Meta()
	rows, err := g.oldCore.query(ctx, "SELECT app, stream FROM events_stream")
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		var app, stream string
		if err := rows.Scan(&app, &stream); err != nil {
			span.SetStatus(codes.Error, "failed to extract results")
			return nil, err
		}

		out.Target = append(out.Target, &ipc.DomainStream{
			Domain: app,
			Stream: stream,
		})
	}
	span.SetAttributes(attribute.Int("streams.count", len(out.Target)))
	return out, rows.Err()
}

func (g *grpcQuery) Get(ctx context.Context, in *ipc.GetIn) (*ipc.GetOut, error) {
	var out = &ipc.GetOut{}
	payload, err := g.oldCore.fetchPayload(ctx, in.Events.Domain, in.Events.Stream, in.Id)
	if err != nil {
		return out, err
	}
	out.Payload = payload
	return out, nil
}

func buildQueryOps(events *ipc.DomainStream, in *ipc.QueryIn) ([]storage2.Operation, error) {
	var ops []storage2.Operation

	var afterID int64
	if in.AfterID != nil {
		afterID = *in.AfterID
	}

	for _, kClause := range in.OnKind {
		if kClause.AllOp != nil {
			ops = append(ops, &storage2.EachKind{
				App:     events.Domain,
				Stream:  events.Stream,
				Op:      int(*kClause.AllOp),
				Kind:    kClause.Kind,
				AfterID: afterID,
			})
		}
		for _, subsetClause := range kClause.Subsets {
			ops = append(ops, &storage2.MatchSubset{
				App:     events.Domain,
				Stream:  events.Stream,
				Op:      int(subsetClause.Op),
				Kind:    kClause.Kind,
				Subset:  json.RawMessage(subsetClause.Match),
				AfterID: afterID,
			})
		}
	}
	for _, idClause := range in.OnID {
		op := storage2.WithMatchID(events.Domain, events.Stream, idClause.Id, int(idClause.Op))
		ops = append(ops, op)
	}
	if eachClause := in.OnEach; eachClause != nil {
		op := &storage2.AllStreamEvents{
			Domain:  events.Domain,
			Stream:  events.Stream,
			Op:      int(eachClause.Op),
			AfterID: afterID,
		}
		ops = append(ops, op)
	}

	if len(ops) == 0 {
		return nil, &v1.EmptyQueryError{}
	}
	return ops, nil
}

func (g *grpcQuery) Query(in *ipc.QueryIn, out ipc.Query_QueryServer) error {
	ops, err := buildQueryOps(in.Events, in)
	if err != nil {
		return err
	}
	if ops == nil {
		return nil
	}

	ctx := out.Context()
	for r, err := range g.core.Stream(ctx, ops) {
		if err != nil {
			return err
		}
		whenTime, err := time.Parse(time.RFC3339Nano, r.Envelope.When)
		if err != nil {
			return err
		}
		if err := out.Send(&ipc.QueryOut{
			Op: int64(r.Op),
			Id: &r.Envelope.ID,
			Envelope: &ipc.MaterializedEnvelope{
				Id:   r.Envelope.ID,
				When: timestamppb.New(whenTime),
				Kind: r.Envelope.Kind,
			},
			Body: r.Event,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (g *grpcQuery) Watch(in *ipc.QueryIn, out ipc.Query_QueryServer) error {
	ctx := out.Context()

	ops, err := buildQueryOps(in.Events, in)
	if err != nil {
		return err
	}
	if ops == nil {
		return nil
	}

	stream := &grpcResultStream{out: out, lastSentID: -1}
	signal := NewRequerySignal(ctx, g.bus.onEventStorage)
	defer signal.Close()

	lastDeliveredID := g.getInitialAfterID(in)

	for {
		if err := signal.Wait(ctx); err != nil {
			return err
		}
		for _, op := range ops {
			op.UpdateAfterID(lastDeliveredID)
		}
		lastID, err := g.processWatchResults(ctx, ops, stream)
		if err != nil {
			return err
		}
		if lastID > lastDeliveredID {
			lastDeliveredID = lastID
		}
	}
}

func (g *grpcQuery) getInitialAfterID(in *ipc.QueryIn) int64 {
	if in.AfterID != nil {
		return *in.AfterID
	}
	return 0
}

func (g *grpcQuery) processWatchResults(ctx context.Context, ops []storage2.Operation, stream *grpcResultStream) (int64, error) {
	var lastID int64
	for r, err := range g.core.Stream(ctx, ops) {
		if err != nil {
			return 0, err
		}
		if err := stream.pushTranslatorMessage(ctx, r); err != nil {
			return 0, err
		}
		if r.Envelope.ID > lastID {
			lastID = r.Envelope.ID
		}
	}
	return lastID, nil
}

// grpcPort exports a Command and Query grpc with the specified configuration
type grpcPort struct {
	config        *GRPCListenerConfig
	oldCore       *storage
	core          *storage2.Repository
	bus           *bus
	consumerStore *storage2.ConsumerStore
	pool          *pgxpool.Pool
}

func (g *grpcPort) Serve(ctx context.Context) error {
	if g.config == nil {
		return suture.ErrDoNotRestart
	}

	opts, err := g.buildServerOptions()
	if err != nil {
		return err
	}

	service := grpc.NewServer(opts...)
	ipc.RegisterCommandServer(service, &grpcCommand{
		oldCore:       g.oldCore,
		core:          g.core,
		bus:           g.bus,
		consumerStore: g.consumerStore,
	})
	ipc.RegisterQueryServer(service, &grpcQuery{
		oldCore: g.oldCore,
		core:    g.core,
		bus:     g.bus,
	})
	ipc.RegisterConsumerPositionServer(service, &grpcConsumerPosition{
		store: g.consumerStore,
	})
	ipc.RegisterConsumerLockServer(service, &grpcConsumerLock{
		consumerStore: g.consumerStore,
		bus:           g.bus,
	})

	// Register ViewProjection services
	if g.pool != nil {
		pgStore := views.NewPGStore(g.pool)
		registry := svviews.NewNotifierRegistry()
		vgrpc.RegisterViewProjectionStoreServer(service, svviews.NewStoreHandler(pgStore, registry))
		vgrpc.RegisterViewProjectionConsumerServer(service, svviews.NewConsumerHandler(pgStore, registry, g.consumerStore))
	}

	tcpListener, err := net.Listen("tcp", g.config.Address)
	if err != nil {
		return err
	}

	return g.runService(ctx, service, tcpListener)
}

func (g *grpcPort) buildServerOptions() ([]grpc.ServerOption, error) {
	var opts []grpc.ServerOption
	opts = append(opts, grpc.StatsHandler(otelgrpc.NewServerHandler()))
	if g.config.ServicePKI != nil {
		keyPair, err := tls.LoadX509KeyPair(g.config.ServicePKI.CertificateFile, g.config.ServicePKI.KeyFile)
		if err != nil {
			return nil, err
		}
		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{keyPair},
			ClientAuth:   tls.NoClientCert,
		}
		opts = append(opts, grpc.Creds(credentials.NewTLS(tlsConfig)))
	}
	return opts, nil
}

func (g *grpcPort) runService(ctx context.Context, service *grpc.Server, tcpListener net.Listener) error {
	listenerResult := make(chan error, 1)
	go func() {
		defer close(listenerResult)
		fmt.Printf("grpc server listening on %s\n", g.config.Address)
		err := service.Serve(tcpListener)
		listenerResult <- err
	}()

	for {
		select {
		case <-ctx.Done():
			return g.handleShutdown(ctx, tcpListener, listenerResult)
		case problem := <-listenerResult:
			return problem
		}
	}
}

func (g *grpcPort) handleShutdown(_ context.Context, tcpListener net.Listener, listenerResult <-chan error) error {
	closeError := tcpListener.Close()
	select {
	case listenerDone := <-listenerResult:
		return errors.Join(closeError, listenerDone)
	case <-time.After(1 * time.Second):
		return errors.Join(errors.New("timed out cleaning up grpc listener"), closeError)
	}
}
