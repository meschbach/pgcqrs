package service

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/jackc/pgx/v5"
	storage2 "github.com/meschbach/pgcqrs/internal/service/storage"
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

type grpcConsumerPosition struct {
	ipc.UnimplementedConsumerPositionServer
	store *storage2.ConsumerStore
}

func (g *grpcConsumerPosition) SetPosition(ctx context.Context, in *ipc.SetPositionIn) (*ipc.SetPositionOut, error) {
	result, err := g.store.SetPosition(ctx, in.Events.Domain, in.Events.Stream, in.Consumer, in.EventID)
	if err != nil {
		return &ipc.SetPositionOut{Ok: false, Error: err.Error()}, nil
	}
	var prevID int64
	if result.PreviousEventID != nil {
		prevID = *result.PreviousEventID
	}
	return &ipc.SetPositionOut{
		Ok:              true,
		CurrentEventID:  result.CurrentEventID,
		PreviousEventID: prevID,
	}, nil
}

func (g *grpcConsumerPosition) GetPosition(ctx context.Context, in *ipc.GetPositionIn) (*ipc.GetPositionOut, error) {
	eventID, found, err := g.store.GetPosition(ctx, in.Events.Domain, in.Events.Stream, in.Consumer)
	if err != nil {
		return nil, err
	}
	return &ipc.GetPositionOut{EventID: eventID, Found: found}, nil
}

func (g *grpcConsumerPosition) ListConsumers(ctx context.Context, in *ipc.ListConsumersIn) (*ipc.ListConsumersOut, error) {
	consumers, err := g.store.ListConsumers(ctx, in.Events.Domain, in.Events.Stream)
	if err != nil {
		return nil, err
	}
	return &ipc.ListConsumersOut{Consumers: consumers}, nil
}

func (g *grpcConsumerPosition) DeletePosition(ctx context.Context, in *ipc.DeletePositionIn) (*ipc.DeletePositionOut, error) {
	err := g.store.DeletePosition(ctx, in.Events.Domain, in.Events.Stream, in.Consumer)
	if err != nil {
		return nil, err
	}
	return &ipc.DeletePositionOut{Ok: true}, nil
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

	var afterID *int64
	if in.AfterID != nil {
		afterID = in.AfterID
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
				// We just need to drain the channel to wait for all listeners to be notified.
				_ = struct{}{}
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

func (g *grpcQuery) createWatchListener(_ context.Context, queryAgain chan<- interface{}) func(context.Context, EventStorageEvent) error {
	return func(ctx context.Context, _ EventStorageEvent) error {
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

// grpcConsumerLock implements the ConsumerLock gRPC service.
type grpcConsumerLock struct {
	ipc.UnimplementedConsumerLockServer
	consumerStore *storage2.ConsumerStore
}

func (g *grpcConsumerLock) TryAcquire(ctx context.Context, in *ipc.TryAcquireIn) (*ipc.TryAcquireOut, error) {
	ctx, span := tracer.Start(ctx, "grpcConsumerLock.TryAcquire", trace.WithAttributes(
		attribute.String("consumer-lock.domain", in.Events.Domain),
		attribute.String("consumer-lock.stream", in.Events.Stream),
		attribute.String("consumer-lock.consumer", in.Consumer),
		attribute.String("consumer-lock.holder", in.Holder),
		attribute.Int("consumer-lock.ttl_seconds", int(in.TtlSeconds)),
	))
	defer span.End()

	ttl := time.Duration(in.TtlSeconds) * time.Second
	result, err := g.consumerStore.TryAcquire(ctx, in.Events.Domain, in.Events.Stream, in.Consumer, in.Holder, ttl)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetAttributes(attribute.Bool("consumer-lock.acquired", result.Acquired))
	out := &ipc.TryAcquireOut{
		Acquired:       result.Acquired,
		HeldBy:         result.HeldBy,
		GuaranteeUntil: timestamppb.New(result.GuaranteeUntil),
		HeldUntil:      timestamppb.New(result.HeldUntil),
	}
	return out, nil
}

func (g *grpcConsumerLock) Release(ctx context.Context, in *ipc.ReleaseIn) (*ipc.ReleaseOut, error) {
	ctx, span := tracer.Start(ctx, "grpcConsumerLock.Release", trace.WithAttributes(
		attribute.String("consumer-lock.domain", in.Events.Domain),
		attribute.String("consumer-lock.stream", in.Events.Stream),
		attribute.String("consumer-lock.consumer", in.Consumer),
		attribute.String("consumer-lock.holder", in.Holder),
	))
	defer span.End()

	err := g.consumerStore.Release(ctx, in.Events.Domain, in.Events.Stream, in.Consumer, in.Holder)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	return &ipc.ReleaseOut{Ok: true}, nil
}

func (g *grpcConsumerLock) KeepAlive(stream grpc.BidiStreamingServer[ipc.KeepAliveClientMessage, ipc.KeepAliveServerMessage]) error {
	ctx := stream.Context()
	var boundHolder string
	var heartbeatDomain, heartbeatStream, heartbeatConsumer string

	for {
		msg, err := stream.Recv()
		if err != nil {
			return g.handleRecvError(ctx, err, heartbeatDomain, heartbeatStream, heartbeatConsumer)
		}

		switch m := msg.Message.(type) {
		case *ipc.KeepAliveClientMessage_Heartbeat:
			hb := m.Heartbeat
			boundHolder, heartbeatDomain, heartbeatStream, heartbeatConsumer, err = g.handleHeartbeatMessage(ctx, stream, hb, boundHolder, heartbeatDomain, heartbeatStream, heartbeatConsumer)
			if err != nil {
				return err
			}
			if boundHolder == "" {
				return nil
			}

		case *ipc.KeepAliveClientMessage_ReleaseRequest:
			return g.handleReleaseRequest(ctx, stream, heartbeatDomain, heartbeatStream, heartbeatConsumer, boundHolder)
		}
	}
}

func (g *grpcConsumerLock) handleHeartbeatMessage(ctx context.Context, stream grpc.BidiStreamingServer[ipc.KeepAliveClientMessage, ipc.KeepAliveServerMessage], hb *ipc.KeepAliveHeartbeat, boundHolder, heartbeatDomain, heartbeatStream, heartbeatConsumer string) (newBoundHolder, newDomain, newStream, newConsumer string, retErr error) {
	if boundHolder == "" {
		heartbeatDomain = hb.Events.Domain
		heartbeatStream = hb.Events.Stream
		heartbeatConsumer = hb.Consumer
		bound, err := g.bindHolder(ctx, stream, hb)
		if err != nil {
			return "", heartbeatDomain, heartbeatStream, heartbeatConsumer, err
		}
		if !bound {
			return "", heartbeatDomain, heartbeatStream, heartbeatConsumer, nil
		}
		boundHolder = hb.Holder
	}
	retErr = g.processHeartbeat(ctx, stream, hb, boundHolder)
	return boundHolder, heartbeatDomain, heartbeatStream, heartbeatConsumer, retErr
}

func (g *grpcConsumerLock) handleReleaseRequest(ctx context.Context, stream grpc.BidiStreamingServer[ipc.KeepAliveClientMessage, ipc.KeepAliveServerMessage], domain, streamName, consumer, holder string) error {
	trace.SpanFromContext(ctx).AddEvent("stream.release_received", trace.WithAttributes(
		attribute.String("consumer-lock.domain", domain),
		attribute.String("consumer-lock.stream", streamName),
		attribute.String("consumer-lock.consumer", consumer),
	))
	err := g.consumerStore.Release(ctx, domain, streamName, consumer, holder)
	if err != nil {
		return err
	}
	return stream.Send(&ipc.KeepAliveServerMessage{
		Message: &ipc.KeepAliveServerMessage_ReleaseAck{
			ReleaseAck: &ipc.KeepAliveReleaseAck{Ok: true},
		},
	})
}

func (g *grpcConsumerLock) handleRecvError(ctx context.Context, err error, domain, stream, consumer string) error {
	if errors.Is(err, io.EOF) {
		if consumer != "" {
			storage2.StreamClosedNoRelease.Add(ctx, 1, metric.WithAttributeSet(attribute.NewSet(
				attribute.String("consumer-lock.domain", domain),
				attribute.String("consumer-lock.stream", stream),
				attribute.String("consumer-lock.consumer", consumer),
			)))
			trace.SpanFromContext(ctx).AddEvent("stream.closed_without_release", trace.WithAttributes(
				attribute.String("consumer-lock.domain", domain),
				attribute.String("consumer-lock.stream", stream),
				attribute.String("consumer-lock.consumer", consumer),
			))
		}
	}
	return err
}

func (g *grpcConsumerLock) bindHolder(ctx context.Context, stream grpc.BidiStreamingServer[ipc.KeepAliveClientMessage, ipc.KeepAliveServerMessage], hb *ipc.KeepAliveHeartbeat) (bool, error) {
	lockState, err := g.consumerStore.GetLock(ctx, hb.Events.Domain, hb.Events.Stream, hb.Consumer)
	if err != nil {
		return false, err
	}
	if lockState == nil {
		trace.SpanFromContext(ctx).AddEvent("heartbeat.expired", trace.WithAttributes(
			attribute.String("consumer-lock.domain", hb.Events.Domain),
			attribute.String("consumer-lock.stream", hb.Events.Stream),
			attribute.String("consumer-lock.consumer", hb.Consumer),
			attribute.String("consumer-lock.holder", hb.Holder),
		))
		return false, stream.Send(&ipc.KeepAliveServerMessage{
			Message: &ipc.KeepAliveServerMessage_LockStatus{
				LockStatus: &ipc.KeepAliveLockStatus{
					Locked: false,
					Reason: ipc.LockStatusReason_EXPIRED,
				},
			},
		})
	}
	if lockState.Holder != hb.Holder {
		trace.SpanFromContext(ctx).AddEvent("heartbeat.stolen", trace.WithAttributes(
			attribute.String("consumer-lock.domain", hb.Events.Domain),
			attribute.String("consumer-lock.stream", hb.Events.Stream),
			attribute.String("consumer-lock.consumer", hb.Consumer),
			attribute.String("consumer-lock.holder", hb.Holder),
			attribute.String("consumer-lock.actual_holder", lockState.Holder),
		))
		return false, stream.Send(&ipc.KeepAliveServerMessage{
			Message: &ipc.KeepAliveServerMessage_LockStatus{
				LockStatus: &ipc.KeepAliveLockStatus{
					Locked: false,
					Reason: ipc.LockStatusReason_STOLEN,
				},
			},
		})
	}
	return true, nil
}

func (g *grpcConsumerLock) processHeartbeat(ctx context.Context, stream grpc.BidiStreamingServer[ipc.KeepAliveClientMessage, ipc.KeepAliveServerMessage], hb *ipc.KeepAliveHeartbeat, boundHolder string) error {
	ctx, span := tracer.Start(ctx, "grpcConsumerLock.Heartbeat", trace.WithAttributes(
		attribute.String("consumer-lock.domain", hb.Events.Domain),
		attribute.String("consumer-lock.stream", hb.Events.Stream),
		attribute.String("consumer-lock.consumer", hb.Consumer),
		attribute.String("consumer-lock.holder", hb.Holder),
		attribute.Int64("consumer-lock.position", hb.Position),
	))
	defer span.End()

	if hb.Holder != boundHolder {
		span.SetAttributes(attribute.String("consumer-lock.status", "STOLEN"))
		span.AddEvent("heartbeat.stolen")
		return stream.Send(&ipc.KeepAliveServerMessage{
			Message: &ipc.KeepAliveServerMessage_LockStatus{
				LockStatus: &ipc.KeepAliveLockStatus{
					Locked: false,
					Reason: ipc.LockStatusReason_STOLEN,
				},
			},
		})
	}

	err := g.consumerStore.HeartbeatWithPosition(ctx, hb.Events.Domain, hb.Events.Stream, hb.Consumer, hb.Holder, hb.Position)
	if err != nil {
		return g.translateHeartbeatError(err, stream, span)
	}

	span.SetAttributes(attribute.String("consumer-lock.status", "RENEWED"))
	span.AddEvent("heartbeat.renewed")
	return stream.Send(&ipc.KeepAliveServerMessage{
		Message: &ipc.KeepAliveServerMessage_LockStatus{
			LockStatus: &ipc.KeepAliveLockStatus{
				Locked: true,
				Reason: ipc.LockStatusReason_RENEWED,
			},
		},
	})
}

func (g *grpcConsumerLock) translateHeartbeatError(err error, stream grpc.BidiStreamingServer[ipc.KeepAliveClientMessage, ipc.KeepAliveServerMessage], span trace.Span) error {
	var conflict *v1.HeartbeatConflictError
	if errors.As(err, &conflict) {
		targetVersion := conflict.TargetVersion
		currentVersion := conflict.CurrentVersion
		span.SetAttributes(
			attribute.String("consumer-lock.status", "CONFLICT"),
			attribute.Int64("consumer-lock.target_version", targetVersion),
			attribute.Int64("consumer-lock.current_version", currentVersion),
		)
		span.AddEvent("heartbeat.conflict", trace.WithAttributes(
			attribute.Int64("consumer-lock.target_version", targetVersion),
			attribute.Int64("consumer-lock.current_version", currentVersion),
		))
		return stream.Send(&ipc.KeepAliveServerMessage{
			Message: &ipc.KeepAliveServerMessage_LockStatus{
				LockStatus: &ipc.KeepAliveLockStatus{
					Locked:         false,
					Reason:         ipc.LockStatusReason_CONFLICT,
					TargetVersion:  &targetVersion,
					CurrentVersion: &currentVersion,
				},
			},
		})
	}
	var lockExpired *v1.LockExpiredError
	if errors.As(err, &lockExpired) {
		span.SetAttributes(attribute.String("consumer-lock.status", "EXPIRED"))
		span.AddEvent("heartbeat.expired")
		return stream.Send(&ipc.KeepAliveServerMessage{
			Message: &ipc.KeepAliveServerMessage_LockStatus{
				LockStatus: &ipc.KeepAliveLockStatus{
					Locked: false,
					Reason: ipc.LockStatusReason_EXPIRED,
				},
			},
		})
	}
	var lockNotFound *storage2.LockNotFoundError
	if errors.As(err, &lockNotFound) {
		span.SetAttributes(attribute.String("consumer-lock.status", "EXPIRED"))
		span.AddEvent("heartbeat.expired")
		return stream.Send(&ipc.KeepAliveServerMessage{
			Message: &ipc.KeepAliveServerMessage_LockStatus{
				LockStatus: &ipc.KeepAliveLockStatus{
					Locked: false,
					Reason: ipc.LockStatusReason_EXPIRED,
				},
			},
		})
	}
	var lockNotHeld *v1.LockNotHeldError
	if errors.As(err, &lockNotHeld) {
		span.SetAttributes(attribute.String("consumer-lock.status", "STOLEN"))
		span.AddEvent("heartbeat.stolen")
		return stream.Send(&ipc.KeepAliveServerMessage{
			Message: &ipc.KeepAliveServerMessage_LockStatus{
				LockStatus: &ipc.KeepAliveLockStatus{
					Locked: false,
					Reason: ipc.LockStatusReason_STOLEN,
				},
			},
		})
	}
	span.SetStatus(codes.Error, err.Error())
	return err
}

func (g *grpcConsumerLock) ListLocks(ctx context.Context, in *ipc.ListLocksIn) (*ipc.ListLocksOut, error) {
	ctx, span := tracer.Start(ctx, "grpcConsumerLock.ListLocks", trace.WithAttributes(
		attribute.String("consumer-lock.domain", in.Events.Domain),
		attribute.String("consumer-lock.stream", in.Events.Stream),
	))
	defer span.End()

	locks, err := g.consumerStore.ListLocks(ctx, in.Events.Domain, in.Events.Stream)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetAttributes(attribute.Int("consumer-lock.lock_count", len(locks)))
	out := &ipc.ListLocksOut{}
	for i := range locks {
		lock := &locks[i]
		out.Locks = append(out.Locks, &ipc.LockState{
			Consumer:       lock.Consumer,
			Domain:         lock.Domain,
			Stream:         lock.Stream,
			Holder:         lock.Holder,
			AcquiredAt:     timestamppb.New(lock.AcquiredAt),
			HeartbeatAt:    timestamppb.New(lock.HeartbeatAt),
			Ttl:            int32(lock.TTL.Seconds()),
			GuaranteeUntil: timestamppb.New(lock.GuaranteeUntil),
			HeldUntil:      timestamppb.New(lock.HeldUntil),
		})
	}
	return out, nil
}

// grpcPort exports a Command and Query grpc with the specified configuration
type grpcPort struct {
	config        *GRPCListenerConfig
	oldCore       *storage
	core          *storage2.Repository
	bus           *bus
	consumerStore *storage2.ConsumerStore
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

func (g *grpcPort) handleShutdown(_ context.Context, tcpListener net.Listener, listenerResult <-chan error) error {
	closeError := tcpListener.Close()
	select {
	case listenerDone := <-listenerResult:
		return errors.Join(closeError, listenerDone)
	case <-time.After(1 * time.Second):
		return errors.Join(errors.New("timed out cleaning up grpc listener"), closeError)
	}
}
