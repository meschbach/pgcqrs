// Package indexer provides a core framework for building event-driven indexers.
// It includes a Pump that drives the event processing loop, Wire and Lock interfaces
// for connecting to pgcqrs, and adapters for gRPC and in-memory transports.
package indexer

import (
	"context"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"google.golang.org/grpc"
)

// GrpcWire wraps a gRPC connection into a Wire.
// Watch, TryAcquire, and GetPosition are promoted from GrpcAdapter via embedding.
// Only NewKeepAlive needs an explicit method (translates KeepAlive bidi stream → Lock).
type GrpcWire struct {
	*v1.GrpcAdapter
}

// GrpcWireConn wraps an existing gRPC connection into a Wire.
func GrpcWireConn(conn *grpc.ClientConn) Wire {
	return &GrpcWire{v1.NewGrpcAdapter(conn)}
}

// NewKeepAlive opens a bidirectional KeepAlive stream and returns a Lock for heartbeating.
func (g *GrpcWire) NewKeepAlive(ctx context.Context, domain, stream, consumer, holder string) (Lock, error) {
	return g.GrpcAdapter.NewKeepAlive(ctx, domain, stream, consumer, holder)
}

// Ensure GrpcWire satisfies Wire at compile time.
var _ Wire = (*GrpcWire)(nil)

// memoryWire wraps a Transport into a Wire (for testing).
type memoryWire struct {
	v1.Transport
}

// MemoryWire wraps a Transport into a Wire.
func MemoryWire(t v1.Transport) Wire {
	return &memoryWire{t}
}

func (m *memoryWire) NewKeepAlive(_ context.Context, domain, stream, consumer, holder string) (Lock, error) {
	return &memoryLock{
		transport: m.Transport,
		domain:    domain,
		stream:    stream,
		consumer:  consumer,
		holder:    holder,
	}, nil
}

// Ensure memoryWire satisfies Wire at compile time.
var _ Wire = (*memoryWire)(nil)

// memoryLock satisfies the Lock interface by delegating to Transport.HeartbeatWithPosition
// and Transport.Release.
type memoryLock struct {
	transport v1.Transport
	domain    string
	stream    string
	consumer  string
	holder    string
}

func (l *memoryLock) Heartbeat(ctx context.Context, position int64) error {
	return l.transport.HeartbeatWithPosition(ctx, l.domain, l.stream, l.consumer, l.holder, position)
}

func (l *memoryLock) Release(ctx context.Context) error {
	return l.transport.Release(ctx, l.domain, l.stream, l.consumer, l.holder)
}

// Ensure memoryLock satisfies Lock at compile time.
var _ Lock = (*memoryLock)(nil)
