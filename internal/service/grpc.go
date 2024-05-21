package service

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
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
	"net"
	"time"
)

type grpcCommand struct {
	ipc.UnimplementedCommandServer
	oldCore *storage
	core    *storage2.Repository
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
	return &ipc.SubmitOut{
		Id:    id,
		State: &ipc.Consistency{After: id},
	}, nil
}

type grpcQuery struct {
	ipc.UnimplementedQueryServer
	oldCore *storage
	core    *storage2.Repository
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
			//return different
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

func (g *grpcQuery) Query(in *ipc.QueryIn, out ipc.Query_QueryServer) error {
	var ops []storage2.Operation

	for _, kClause := range in.OnKind {
		if kClause.AllOp != nil {
			ops = append(ops, &storage2.EachKind{
				App:    in.Events.Domain,
				Stream: in.Events.Stream,
				Op:     int(*kClause.AllOp),
				Kind:   kClause.Kind,
			})
		}
		for _, subsetClause := range kClause.Subsets {
			ops = append(ops, &storage2.MatchSubset{
				App:    in.Events.Domain,
				Stream: in.Events.Stream,
				Op:     int(subsetClause.Op),
				Kind:   kClause.Kind,
				Subset: subsetClause.Match,
			})
		}
	}
	for _, idClause := range in.OnID {
		op := storage2.WithMatchID(in.Events.Domain, in.Events.Stream, idClause.Id, int(idClause.Op))
		ops = append(ops, op)
	}
	if eachClause := in.OnEach; eachClause != nil {
		op := &storage2.AllStreamEvents{
			Domain: in.Events.Domain,
			Stream: in.Events.Stream,
			Op:     int(eachClause.Op),
		}
		ops = append(ops, op)
	}

	if len(ops) == 0 {
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
			//discard all remaining results
			for range results {
			}
		}
		for r := range results {
			//todo(optimization): we are translating from PG's time to a string to gRPC.  PG gives us a time.Time.
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

// grpcPort exports a Command and Query grpc with the specified configuration
type grpcPort struct {
	config  *GRPCListenerConfig
	oldCore *storage
	core    *storage2.Repository
}

func (g *grpcPort) Serve(ctx context.Context) error {
	//Just exit without a proper configuration
	if g.config == nil {
		return suture.ErrDoNotRestart
	}
	//build service
	var opts []grpc.ServerOption
	opts = append(opts, grpc.StatsHandler(otelgrpc.NewServerHandler()))
	if g.config.ServicePKI != nil {
		keyPair, err := tls.LoadX509KeyPair(g.config.ServicePKI.CertificateFile, g.config.ServicePKI.KeyFile)
		if err != nil {
			return err
		}
		config := &tls.Config{
			Certificates: []tls.Certificate{keyPair},
			ClientAuth:   tls.NoClientCert,
		}
		opts = append(opts, grpc.Creds(credentials.NewTLS(config)))
	}
	service := grpc.NewServer(opts...)
	ipc.RegisterCommandServer(service, &grpcCommand{
		oldCore: g.oldCore,
		core:    g.core,
	})
	ipc.RegisterQueryServer(service, &grpcQuery{
		oldCore: g.oldCore,
		core:    g.core,
	})

	//
	tcpListener, err := net.Listen("tcp", g.config.Address)
	if err != nil {
		return err
	}

	//launch the service
	listenerResult := make(chan error, 1)
	go func() {
		defer close(listenerResult)
		fmt.Printf("grpc server listening on %s\n", g.config.Address)
		err := service.Serve(tcpListener)
		listenerResult <- err
	}()

	//Wait for either (1) the service to be shutdown or (2) a problem serving
	for {
		select {
		case <-ctx.Done():
			closeError := tcpListener.Close()
			select {
			case listenerDone := <-listenerResult:
				return errors.Join(closeError, listenerDone)
			case <-time.After(1 * time.Second):
				return errors.Join(errors.New("timed out cleaning up grpc listener"), closeError)
			}
		case problem := <-listenerResult:
			return problem
		}
	}
}
