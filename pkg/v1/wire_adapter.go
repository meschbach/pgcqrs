package v1

import (
	"context"
	"time"

	"github.com/meschbach/pgcqrs/pkg/ipc"
	"google.golang.org/grpc"
)

// GrpcWire wraps a gRPC connection into a Wire.
type GrpcWire struct {
	*GrpcAdapter
}

// NewGrpcWire creates a GrpcWire from a gRPC connection.
func NewGrpcWire(conn *grpc.ClientConn) *GrpcWire {
	return &GrpcWire{NewGrpcAdapter(conn)}
}

// WaitForLock calls the WaitForLock RPC and returns the lock, position, and any error.
func (g *GrpcWire) WaitForLock(ctx context.Context, domain, stream, consumer, holder string, ttl time.Duration) (*KeepAlive, int64, error) {
	req := &ipc.WaitForLockRequest{
		Events: &ipc.DomainStream{
			Domain: domain,
			Stream: stream,
		},
		Consumer:   consumer,
		Holder:     holder,
		TtlSeconds: int32(ttl.Seconds()),
	}
	grpcStream, err := g.locks.WaitForLock(ctx, req)
	if err != nil {
		return nil, 0, err
	}
	resp, err := grpcStream.Recv()
	if err != nil {
		return nil, 0, err
	}
	keepAlive, err := g.NewKeepAlive(ctx, domain, stream, consumer, holder)
	if err != nil {
		return nil, 0, err
	}
	return keepAlive, resp.Position, nil
}

// MemoryWire wraps a Transport into a Wire (for testing).
type MemoryWire struct {
	Transport
}

// NewMemoryWire creates a MemoryWire from a Transport.
func NewMemoryWire(t Transport) *MemoryWire {
	return &MemoryWire{t}
}

// WaitForLock waits for the lock to be available using the emitter, then acquires it.
func (m *MemoryWire) WaitForLock(ctx context.Context, domain, stream, consumer, holder string, ttl time.Duration) (*MemoryLock, int64, error) {
	for {
		lock, position, acquired, err := m.tryAcquireWithPosition(ctx, domain, stream, consumer, holder, ttl)
		if err != nil {
			return nil, 0, err
		}
		if acquired {
			return lock, position, nil
		}
		if err := m.waitForRelease(ctx, domain, stream, consumer); err != nil {
			return nil, 0, err
		}
	}
}

func (m *MemoryWire) tryAcquireWithPosition(ctx context.Context, domain, stream, consumer, holder string, ttl time.Duration) (lock *MemoryLock, position int64, acquired bool, err error) {
	result, err := m.TryAcquire(ctx, domain, stream, consumer, holder, ttl)
	if err != nil {
		return nil, 0, false, err
	}
	if !result.Acquired {
		return nil, 0, false, nil
	}
	position, _, err = m.GetPosition(ctx, domain, stream, consumer)
	if err != nil {
		return nil, 0, false, err
	}
	return &MemoryLock{
		transport: m.Transport,
		domain:    domain,
		stream:    stream,
		consumer:  consumer,
		holder:    holder,
	}, position, true, nil
}

// waitForRelease blocks until the consumer's lock is released or the context
// is canceled.
func (m *MemoryWire) waitForRelease(ctx context.Context, domain, stream, consumer string) error {
	ch, unsub := subscribeToRelease(m.Transport, domain, stream, consumer)
	defer unsub()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
		return nil
	}
}

// subscribeToRelease registers a callback that signals releaseCh when a lock
// for the consumer is released. Returns an unsubscribe function.
func subscribeToRelease(transport Transport, domain, stream, consumer string) (releaseCh chan struct{}, unsub func()) {
	releaseCh = make(chan struct{}, 1)
	unsub = transport.OnLockRelease(func(_ context.Context, evt LockReleasedEvent) error {
		if evt.Domain == domain && evt.Stream == stream && evt.Consumer == consumer {
			select {
			case releaseCh <- struct{}{}:
			default:
			}
		}
		return nil
	})
	return
}

// MemoryLock satisfies the Lock interface by delegating to Transport.HeartbeatWithPosition
// and Transport.Release.
type MemoryLock struct {
	transport Transport
	domain    string
	stream    string
	consumer  string
	holder    string
}

// Heartbeat renews the lock and records the consumer's position.
func (l *MemoryLock) Heartbeat(ctx context.Context, position int64) error {
	return l.transport.HeartbeatWithPosition(ctx, l.domain, l.stream, l.consumer, l.holder, position)
}

// Release releases the lock held by this MemoryLock.
func (l *MemoryLock) Release(ctx context.Context) error {
	return l.transport.Release(ctx, l.domain, l.stream, l.consumer, l.holder)
}
