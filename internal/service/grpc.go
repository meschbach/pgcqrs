package service

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	storage2 "github.com/meschbach/pgcqrs/internal/service/storage"
	"github.com/meschbach/pgcqrs/pkg/ipc"
	"github.com/thejerf/suture/v4"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type grpcCommand struct {
	ipc.UnimplementedCommandServer
	oldCore *storage
	core    *storage2.Repository
	bus     *bus
}

func (g *grpcCommand) CreateStream(ctx context.Context, in *ipc.CreateStreamIn) (*ipc.CreateStreamOut, error) {
	err := g.oldCore.ensureStream(ctx, in.Target.Domain, in.Target.Stream)
	return &ipc.CreateStreamOut{}, err
}

func (g *grpcCommand) Submit(ctx context.Context, in *ipc.SubmitIn) (*ipc.SubmitOut, error) {
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

type grpcQuery struct {
	ipc.UnimplementedQueryServer
	oldCore *storage
	core    *storage2.Repository
	bus     *bus
}

func (g *grpcQuery) ListStreams(ctx context.Context, in *ipc.ListStreamsIn) (*ipc.ListStreamsOut, error) {
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

	for _, kClause := range in.OnKind {
		if kClause.AllOp != nil {
			ops = append(ops, &storage2.EachKind{
				App:    events.Domain,
				Stream: events.Stream,
				Op:     int(*kClause.AllOp),
				Kind:   kClause.Kind,
			})
		}
		for _, subsetClause := range kClause.Subsets {
			ops = append(ops, &storage2.MatchSubset{
				App:    events.Domain,
				Stream: events.Stream,
				Op:     int(subsetClause.Op),
				Kind:   kClause.Kind,
				Subset: json.RawMessage(subsetClause.Match),
			})
		}
	}
	for _, idClause := range in.OnID {
		op := storage2.WithMatchID(events.Domain, events.Stream, idClause.Id, int(idClause.Op))
		ops = append(ops, op)
	}
	if eachClause := in.OnEach; eachClause != nil {
		op := &storage2.AllStreamEvents{
			Domain: events.Domain,
			Stream: events.Stream,
			Op:     int(eachClause.Op),
		}
		ops = append(ops, op)
	}

	if len(ops) == 0 {
		return nil, nil
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

	results, start, err := g.core.Stream(out.Context(), ops)
	if err != nil {
		return err
	}

	type translateResult struct {
		problem error
	}
	translateOut := make(chan translateResult, 1)
	go func() {
		defer close(translateOut)
		onError := func(err error) {
			translateOut <- translateResult{problem: err}
			// discard all remaining results
			for range results {
			}
		}
		for r := range results {
			// todo(optimization): we are translating from PG's time to a string to gRPC.  PG gives us a time.Time.
			whenTime, err := time.Parse(time.RFC3339Nano, r.Envelope.When)
			if err != nil {
				onError(err)
				return
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
				onError(err)
				return
			}
		}
	}()

	type runResult struct {
		count   int
		problem error
	}
	runOut := make(chan runResult, 1)
	go func() {
		count, err := start(out.Context())
		runOut <- runResult{count, err}
		close(runOut)
	}()

	run := <-runOut
	translator := <-translateOut
	return errors.Join(run.problem, translator.problem)
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

	results := make(chan storage2.OperationResult, 128)
	translateOut := make(chan error, 1)
	stream := &grpcResultStream{out: out}
	go stream.runTranslator(ctx, results, translateOut)

	queryAgain := make(chan interface{}, 1)
	queryAgain <- nil
	watcher := g.bus.onEventStorage.OnE(g.createWatchListener(ctx, queryAgain))
	defer watcher.Off()

	for {
		select {
		case <-ctx.Done():
			translator := <-translateOut
			return errors.Join(translator, ctx.Err())
		case <-queryAgain:
			if err := g.runWatchQuery(ctx, out, ops, results); err != nil {
				return err
			}
		}
	}
}

func (g *grpcQuery) createWatchListener(ctx context.Context, queryAgain chan<- interface{}) func(context.Context, EventStorageEvent) error {
	return func(ctx context.Context, storage EventStorageEvent) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case queryAgain <- nil:
			return nil
		default:
			return nil
		}
	}
}

func (g *grpcQuery) runWatchQuery(ctx context.Context, out ipc.Query_QueryServer, ops []storage2.Operation, results chan<- storage2.OperationResult) error {
	type runResult struct {
		count   int
		problem error
	}

	queryResults, start, err := g.core.Stream(out.Context(), ops)
	if err != nil {
		return err
	}

	go func() {
		for r := range queryResults {
			results <- r
		}
	}()

	runOut := make(chan runResult, 1)
	go func() {
		count, err := start(out.Context())
		runOut <- runResult{count, err}
		close(runOut)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case runErr := <-runOut:
		if runErr.problem != nil {
			return runErr.problem
		}
	}
	return nil
}

// grpcPort exports a Command and Query grpc with the specified configuration
type grpcPort struct {
	config  *GRPCListenerConfig
	oldCore *storage
	core    *storage2.Repository
	bus     *bus
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
		oldCore: g.oldCore,
		core:    g.core,
		bus:     g.bus,
	})
	ipc.RegisterQueryServer(service, &grpcQuery{
		oldCore: g.oldCore,
		core:    g.core,
		bus:     g.bus,
	})

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

func (g *grpcPort) handleShutdown(ctx context.Context, tcpListener net.Listener, listenerResult <-chan error) error {
	closeError := tcpListener.Close()
	select {
	case listenerDone := <-listenerResult:
		return errors.Join(closeError, listenerDone)
	case <-time.After(1 * time.Second):
		return errors.Join(errors.New("timed out cleaning up grpc listener"), closeError)
	}
}
