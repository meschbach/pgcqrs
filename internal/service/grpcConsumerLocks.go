package service

import (
	"context"
	"errors"
	"io"
	"time"

	storage2 "github.com/meschbach/pgcqrs/internal/service/storage"
	"github.com/meschbach/pgcqrs/pkg/ipc"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// grpcConsumerLock implements the ConsumerLock gRPC service.
type grpcConsumerLock struct {
	ipc.UnimplementedConsumerLockServer
	consumerStore *storage2.ConsumerStore
	bus           *bus
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
	result, expiredLocks, err := g.consumerStore.TryAcquire(ctx, in.Events.Domain, in.Events.Stream, in.Consumer, in.Holder, ttl)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	// Emit events for expired locks
	for _, expired := range expiredLocks {
		go g.bus.dispatchOnLockReleased(ctx, expired.Domain, expired.Stream, expired.Consumer, expired.Holder)
	}

	span.SetAttributes(attribute.Bool("consumer-lock.acquired", result.Acquired))
	out := &ipc.TryAcquireOut{
		Acquired:       result.Acquired,
		HeldBy:         result.HeldBy,
		GuaranteeUntil: timestamppb.New(result.GuaranteeUntil),
		HeldUntil:      timestamppb.New(result.HeldUntil),
		Position:       result.Position,
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

	released, err := g.consumerStore.Release(ctx, in.Events.Domain, in.Events.Stream, in.Consumer, in.Holder)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	// Emit event for released lock
	if released != nil {
		go g.bus.dispatchOnLockReleased(ctx, released.Domain, released.Stream, released.Consumer, released.Holder)
	}

	return &ipc.ReleaseOut{Ok: true}, nil
}

// keepAliveStream models a live KeepAlive bidirectional stream as a small state
// machine. The stream starts unbound and must receive a Bind message before any
// heartbeat or release request is accepted.
type keepAliveStream struct {
	server *grpcConsumerLock
	stream grpc.BidiStreamingServer[ipc.KeepAliveClientMessage, ipc.KeepAliveServerMessage]

	domain     string
	streamName string
	consumer   string
	holder     string
	unsub      func()

	lockLostCh  chan LockReleasedEvent
	idleTimer   *time.Timer
	idleTimeout time.Duration
}

func newKeepAliveStream(server *grpcConsumerLock, stream grpc.BidiStreamingServer[ipc.KeepAliveClientMessage, ipc.KeepAliveServerMessage]) *keepAliveStream {
	return &keepAliveStream{
		server:      server,
		stream:      stream,
		lockLostCh:  make(chan LockReleasedEvent, 1),
		idleTimeout: 2 * v1.DefaultLockTTL,
	}
}

func (g *grpcConsumerLock) KeepAlive(stream grpc.BidiStreamingServer[ipc.KeepAliveClientMessage, ipc.KeepAliveServerMessage]) error {
	return newKeepAliveStream(g, stream).run()
}

func (k *keepAliveStream) run() error {
	k.idleTimer = time.NewTimer(k.idleTimeout)
	defer k.idleTimer.Stop()
	defer k.close()

	bound, err := k.awaitBind()
	if err != nil {
		return err
	}
	if !bound {
		return nil
	}
	return k.runBound()
}

// awaitBind reads until the initial Bind message arrives, rejecting any other
// message type. The idle timeout still applies so unbound streams cannot leak.
// Returns false when the stream should close without entering the bound phase.
func (k *keepAliveStream) awaitBind() (bool, error) {
	for {
		msg, done, err := k.receive()
		if err != nil {
			return false, err
		}
		if done {
			return false, nil
		}
		switch m := msg.Message.(type) {
		case *ipc.KeepAliveClientMessage_Bind:
			done, err := k.bind(m.Bind)
			if err != nil {
				return false, err
			}
			return !done, nil
		case *ipc.KeepAliveClientMessage_Heartbeat:
			return false, status.Error(grpccodes.InvalidArgument, "heartbeat received before bind")
		case *ipc.KeepAliveClientMessage_ReleaseRequest:
			return false, status.Error(grpccodes.InvalidArgument, "release received before bind")
		default:
			return false, status.Error(grpccodes.InvalidArgument, "unexpected message type on keepalive stream")
		}
	}
}

// runBound processes heartbeats and release requests once the stream is bound
// to a lock.
func (k *keepAliveStream) runBound() error {
	for {
		msg, done, err := k.receive()
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		if err := k.dispatchBound(msg); err != nil {
			return err
		}
	}
}

// dispatchBound routes a bound-stream message. The Bind case is rejected as a
// rebind.
func (k *keepAliveStream) dispatchBound(msg *ipc.KeepAliveClientMessage) error {
	switch m := msg.Message.(type) {
	case *ipc.KeepAliveClientMessage_Heartbeat:
		return k.handleHeartbeat(m.Heartbeat)
	case *ipc.KeepAliveClientMessage_ReleaseRequest:
		return k.server.handleReleaseRequest(k.stream.Context(), k.stream, k.domain, k.streamName, k.consumer, k.holder)
	case *ipc.KeepAliveClientMessage_Bind:
		return status.Errorf(grpccodes.InvalidArgument, "cannot rebind: stream already bound to holder %s", k.holder)
	default:
		return status.Error(grpccodes.InvalidArgument, "unexpected message type on keepalive stream")
	}
}

// handleHeartbeat rejects heartbeats whose domain/stream/consumer headers do
// not match the bound lock.
func (k *keepAliveStream) handleHeartbeat(hb *ipc.KeepAliveHeartbeat) error {
	if hb.Events.Domain != k.domain || hb.Events.Stream != k.streamName || hb.Consumer != k.consumer {
		return &v1.LockNotHeldError{
			Consumer: k.consumer,
			Holder:   k.holder,
			Domain:   k.domain,
			Stream:   k.streamName,
		}
	}
	return k.server.processHeartbeat(k.stream.Context(), k.stream, hb, k.holder)
}

// bind validates the bind request, resolves the lock, and transitions the
// stream to the bound state. Returns done=true when the lock is not held and
// the stream should close.
func (k *keepAliveStream) bind(req *ipc.KeepAliveBindRequest) (bool, error) {
	if req.Domain == "" || req.Stream == "" || req.Consumer == "" || req.Holder == "" {
		return false, status.Error(grpccodes.InvalidArgument, "bind message must include domain, stream, consumer, and holder")
	}

	k.domain = req.Domain
	k.streamName = req.Stream
	k.consumer = req.Consumer
	k.holder = req.Holder

	bound, err := k.server.bindHolder(k.stream.Context(), k.stream, req.Domain, req.Stream, req.Consumer, req.Holder)
	if err != nil {
		return false, err
	}
	if !bound {
		return true, nil
	}

	// Subscribe to lock release events so release notifications are delivered
	// even without a prior heartbeat.
	k.subscribeToLockReleased()

	return false, k.sendLocked()
}

func (k *keepAliveStream) subscribeToLockReleased() {
	sub := k.server.bus.onLockRelease.OnE(func(_ context.Context, evt LockReleasedEvent) error {
		if evt.Domain == k.domain && evt.Stream == k.streamName && evt.Consumer == k.consumer && evt.Holder == k.holder {
			select {
			case k.lockLostCh <- evt:
			default:
			}
		}
		return nil
	})
	k.unsub = func() {
		k.server.bus.onLockRelease.Off(sub)
	}
}

func (k *keepAliveStream) sendLocked() error {
	return k.stream.Send(&ipc.KeepAliveServerMessage{
		Message: &ipc.KeepAliveServerMessage_LockStatus{
			LockStatus: &ipc.KeepAliveLockStatus{
				Locked: true,
			},
		},
	})
}

func (k *keepAliveStream) close() {
	if k.unsub != nil {
		k.unsub()
	}
}

// receive blocks until a client message arrives, the stream idles out, or the
// bound lock is released. Returns done=true when the stream should close
// normally.
func (k *keepAliveStream) receive() (*ipc.KeepAliveClientMessage, bool, error) {
	type recvResult struct {
		msg *ipc.KeepAliveClientMessage
		err error
	}
	recvCh := make(chan recvResult, 1)
	go func() {
		msg, err := k.stream.Recv()
		recvCh <- recvResult{msg: msg, err: err}
	}()

	select {
	case evt := <-k.lockLostCh:
		return nil, true, k.notifyLockLost(evt)
	case result := <-recvCh:
		if !k.idleTimer.Stop() {
			<-k.idleTimer.C
		}
		k.idleTimer.Reset(k.idleTimeout)
		if result.err != nil {
			return nil, false, k.server.handleRecvError(k.stream.Context(), result.err, k.domain, k.streamName, k.consumer)
		}
		return result.msg, false, nil
	case <-k.idleTimer.C:
		trace.SpanFromContext(k.stream.Context()).AddEvent("stream.idle_timeout", trace.WithAttributes(
			attribute.String("consumer-lock.domain", k.domain),
			attribute.String("consumer-lock.stream", k.streamName),
			attribute.String("consumer-lock.consumer", k.consumer),
			attribute.String("consumer-lock.holder", k.holder),
		))
		return nil, true, nil
	}
}

// notifyLockLost sends a LockLost notification for a lock release event.
func (k *keepAliveStream) notifyLockLost(evt LockReleasedEvent) error {
	if err := k.stream.Send(&ipc.KeepAliveServerMessage{
		Message: &ipc.KeepAliveServerMessage_LockLost{
			LockLost: &ipc.LockLost{
				Reason: "released",
			},
		},
	}); err != nil {
		return err
	}
	trace.SpanFromContext(k.stream.Context()).AddEvent("stream.lock_lost", trace.WithAttributes(
		attribute.String("consumer-lock.domain", evt.Domain),
		attribute.String("consumer-lock.stream", evt.Stream),
		attribute.String("consumer-lock.consumer", evt.Consumer),
		attribute.String("consumer-lock.holder", evt.Holder),
	))
	return nil
}

func (g *grpcConsumerLock) handleReleaseRequest(ctx context.Context, stream grpc.BidiStreamingServer[ipc.KeepAliveClientMessage, ipc.KeepAliveServerMessage], domain, streamName, consumer, holder string) error {
	trace.SpanFromContext(ctx).AddEvent("stream.release_received", trace.WithAttributes(
		attribute.String("consumer-lock.domain", domain),
		attribute.String("consumer-lock.stream", streamName),
		attribute.String("consumer-lock.consumer", consumer),
	))
	// Defensive validation: the bind-first ordering guarantees non-empty values via KeepAlive,
	// but reject an unbound release rather than silently succeeding.
	if domain == "" || streamName == "" || consumer == "" || holder == "" {
		return stream.Send(&ipc.KeepAliveServerMessage{
			Message: &ipc.KeepAliveServerMessage_ReleaseAck{
				ReleaseAck: &ipc.KeepAliveReleaseAck{Ok: false},
			},
		})
	}
	released, err := g.consumerStore.Release(ctx, domain, streamName, consumer, holder)
	if err != nil {
		return err
	}
	// Emit event for released lock
	if released != nil {
		go g.bus.dispatchOnLockReleased(ctx, released.Domain, released.Stream, released.Consumer, released.Holder)
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

func (g *grpcConsumerLock) bindHolder(ctx context.Context, stream grpc.BidiStreamingServer[ipc.KeepAliveClientMessage, ipc.KeepAliveServerMessage], domain, streamName, consumer, holder string) (bool, error) {
	lockState, found, err := g.consumerStore.GetLock(ctx, domain, streamName, consumer)
	if err != nil {
		return false, err
	}
	if !found {
		trace.SpanFromContext(ctx).AddEvent("bind.expired", trace.WithAttributes(
			attribute.String("consumer-lock.domain", domain),
			attribute.String("consumer-lock.stream", streamName),
			attribute.String("consumer-lock.consumer", consumer),
			attribute.String("consumer-lock.holder", holder),
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
	if lockState.Holder != holder {
		trace.SpanFromContext(ctx).AddEvent("bind.stolen", trace.WithAttributes(
			attribute.String("consumer-lock.domain", domain),
			attribute.String("consumer-lock.stream", streamName),
			attribute.String("consumer-lock.consumer", consumer),
			attribute.String("consumer-lock.holder", holder),
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
	var lockNotFound *v1.LockNotFoundError
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

func (g *grpcConsumerLock) WaitForLock(req *ipc.WaitForLockRequest, stream grpc.ServerStreamingServer[ipc.LockGranted]) error {
	ctx := stream.Context()
	ctx, span := tracer.Start(ctx, "grpcConsumerLock.WaitForLock", trace.WithAttributes(
		attribute.String("consumer-lock.domain", req.Events.Domain),
		attribute.String("consumer-lock.stream", req.Events.Stream),
		attribute.String("consumer-lock.consumer", req.Consumer),
		attribute.String("consumer-lock.holder", req.Holder),
		attribute.Int("consumer-lock.ttl_seconds", int(req.TtlSeconds)),
	))
	defer span.End()

	ttl := time.Duration(req.TtlSeconds) * time.Second

	// Try to acquire immediately
	result, err := g.acquireLock(ctx, req.Events.Domain, req.Events.Stream, req.Consumer, req.Holder, ttl, span)
	if err != nil {
		return err
	}
	if result.Acquired {
		return g.grantLock(stream, result.Position, ttl)
	}

	// Lock is held by someone else, wait for a release notification
	return g.waitForGrant(ctx, stream, req.Events.Domain, req.Events.Stream, req.Consumer, req.Holder, ttl, span)
}

// waitForGrant blocks until the lock is acquired or the context is canceled,
// retrying acquisition whenever a release notification arrives.
func (g *grpcConsumerLock) waitForGrant(ctx context.Context, stream grpc.ServerStreamingServer[ipc.LockGranted], domain, streamName, consumer, holder string, ttl time.Duration, span trace.Span) error {
	releaseCh := make(chan struct{}, 1)
	unsub := g.subscribeToRelease(domain, streamName, consumer, releaseCh)
	defer unsub()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-releaseCh:
			result, err := g.acquireLock(ctx, domain, streamName, consumer, holder, ttl, span)
			if err != nil {
				return err
			}
			if result.Acquired {
				return g.grantLock(stream, result.Position, ttl)
			}
			// Still not acquired, continue waiting
		}
	}
}

// subscribeToRelease registers a callback that signals releaseCh when a lock
// for the consumer is released. Returns an unsubscribe function.
func (g *grpcConsumerLock) subscribeToRelease(domain, streamName, consumer string, releaseCh chan<- struct{}) func() {
	sub := g.bus.onLockRelease.OnE(func(_ context.Context, evt LockReleasedEvent) error {
		if evt.Domain == domain && evt.Stream == streamName && evt.Consumer == consumer {
			select {
			case releaseCh <- struct{}{}:
			default:
			}
		}
		return nil
	})
	return func() {
		g.bus.onLockRelease.Off(sub)
	}
}

// acquireLock attempts to acquire a lock, dispatching release notifications for
// any expired locks cleaned up along the way.
func (g *grpcConsumerLock) acquireLock(ctx context.Context, domain, stream, consumer, holder string, ttl time.Duration, span trace.Span) (*v1.LockResult, error) {
	result, expiredLocks, err := g.consumerStore.TryAcquire(ctx, domain, stream, consumer, holder, ttl)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	for _, expired := range expiredLocks {
		go g.bus.dispatchOnLockReleased(ctx, expired.Domain, expired.Stream, expired.Consumer, expired.Holder)
	}
	return result, nil
}

// grantLock sends a LockGranted response with the heartbeat interval derived
// from the lock TTL and the consumer's stored position.
func (g *grpcConsumerLock) grantLock(stream grpc.ServerStreamingServer[ipc.LockGranted], position int64, ttl time.Duration) error {
	heartbeatInterval := time.Duration(float64(ttl) * v1.DefaultGuaranteeFraction)
	return stream.Send(&ipc.LockGranted{
		Position:            position,
		HeartbeatIntervalMs: heartbeatInterval.Milliseconds(),
		DeadlineMs:          ttl.Milliseconds(),
	})
}
