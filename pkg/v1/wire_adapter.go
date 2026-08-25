package v1

import (
	"context"
	"fmt"
	"time"

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

// WaitForLock calls the WaitForLock RPC and returns the lock, position, heartbeat interval, and any error.
func (g *GrpcWire) WaitForLock(ctx context.Context, domain, stream, consumer, holder string, ttl time.Duration) (*KeepAlive, int64, time.Duration, error) {
	lease, position, heartbeatInterval, err := g.GrpcAdapter.WaitForLock(ctx, domain, stream, consumer, holder, ttl)
	if err != nil {
		return nil, 0, 0, err
	}
	keepAlive, ok := lease.(*KeepAlive)
	if !ok {
		return nil, 0, 0, fmt.Errorf("unexpected lease type %T", lease)
	}
	return keepAlive, position, heartbeatInterval, nil
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
// Uses the register-then-recheck pattern: subscribe to release notifications before
// the first acquire attempt, then recheck after each notification to avoid missing
// releases that fire between a failed acquire and subscription.
func (m *MemoryWire) WaitForLock(ctx context.Context, domain, stream, consumer, holder string, ttl time.Duration) (*MemoryLock, int64, time.Duration, error) {
	waiter, ok := m.Transport.(interface {
		WaitForLock(ctx context.Context, domain, stream, consumer, holder string, ttl time.Duration) (LockLease, int64, time.Duration, error)
	})
	if !ok {
		return nil, 0, 0, fmt.Errorf("transport does not support WaitForLock")
	}
	lease, position, heartbeatInterval, err := waiter.WaitForLock(ctx, domain, stream, consumer, holder, ttl)
	if err != nil {
		return nil, 0, 0, err
	}
	lock, ok := lease.(*MemoryLock)
	if !ok {
		return nil, 0, 0, fmt.Errorf("unexpected lease type %T", lease)
	}
	return lock, position, heartbeatInterval, nil
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
