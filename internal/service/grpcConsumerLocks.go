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
	"google.golang.org/protobuf/types/known/timestamppb"
)

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

	idleTimeout := 2 * v1.DefaultLockTTL
	idleTimer := time.NewTimer(idleTimeout)
	defer idleTimer.Stop()

	for {
		msg, timedOut, err := g.recvWithTimeout(stream, idleTimer, idleTimeout)
		if timedOut {
			trace.SpanFromContext(ctx).AddEvent("stream.idle_timeout", trace.WithAttributes(
				attribute.String("consumer-lock.domain", heartbeatDomain),
				attribute.String("consumer-lock.stream", heartbeatStream),
				attribute.String("consumer-lock.consumer", heartbeatConsumer),
				attribute.String("consumer-lock.holder", boundHolder),
			))
			return nil
		}
		if err != nil {
			return g.handleRecvError(ctx, err, heartbeatDomain, heartbeatStream, heartbeatConsumer)
		}

		switch m := msg.Message.(type) {
		case *ipc.KeepAliveClientMessage_Heartbeat:
			hb := m.Heartbeat
			var err error
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

func (g *grpcConsumerLock) recvWithTimeout(
	stream grpc.BidiStreamingServer[ipc.KeepAliveClientMessage, ipc.KeepAliveServerMessage],
	timer *time.Timer,
	timeout time.Duration,
) (*ipc.KeepAliveClientMessage, bool, error) {
	type recvResult struct {
		msg *ipc.KeepAliveClientMessage
		err error
	}
	recvCh := make(chan recvResult, 1)
	go func() {
		msg, err := stream.Recv()
		recvCh <- recvResult{msg: msg, err: err}
	}()

	select {
	case result := <-recvCh:
		if !timer.Stop() {
			<-timer.C
		}
		timer.Reset(timeout)
		return result.msg, false, result.err
	case <-timer.C:
		return nil, true, nil
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
	} else if hb.Events.Domain != heartbeatDomain || hb.Events.Stream != heartbeatStream || hb.Consumer != heartbeatConsumer {
		trace.SpanFromContext(ctx).AddEvent("stream.binding_mismatch", trace.WithAttributes(
			attribute.String("consumer-lock.expected_domain", heartbeatDomain),
			attribute.String("consumer-lock.expected_stream", heartbeatStream),
			attribute.String("consumer-lock.expected_consumer", heartbeatConsumer),
			attribute.String("consumer-lock.actual_domain", hb.Events.Domain),
			attribute.String("consumer-lock.actual_stream", hb.Events.Stream),
			attribute.String("consumer-lock.actual_consumer", hb.Consumer),
		))
		return boundHolder, heartbeatDomain, heartbeatStream, heartbeatConsumer, &v1.LockNotHeldError{
			Consumer: heartbeatConsumer,
			Holder:   boundHolder,
			Domain:   heartbeatDomain,
			Stream:   heartbeatStream,
		}
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
	lockState, found, err := g.consumerStore.GetLock(ctx, hb.Events.Domain, hb.Events.Stream, hb.Consumer)
	if err != nil {
		return false, err
	}
	if !found {
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
