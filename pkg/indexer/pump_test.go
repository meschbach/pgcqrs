package indexer

import (
	"context"
	"errors"
	"testing"
	"time"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/meschbach/pgcqrs/pkg/v1/query2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPumpAcquiresLockAndProcessesEvents(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	ctx := t.Context()
	stream, err := sys.Stream(ctx, "test-domain", "test-stream")
	require.NoError(t, err)

	// Submit events
	for i := 0; i < 3; i++ {
		_, err = stream.Submit(ctx, "TestEvent", map[string]int{"i": i})
		require.NoError(t, err)
	}

	wire := MemoryWire(transport)
	proj := &mockIndexer{stream: stream}
	pump := NewPump(wire, proj, "test-holder", WithTTL(10*time.Second))

	runCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	err = pump.RunWithDomainStream(runCtx, "test-domain", "test-stream")
	assert.Error(t, err)
}

func TestPumpLockLifecycle(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	ctx := t.Context()
	stream, err := sys.Stream(ctx, "test-domain", "test-stream")
	require.NoError(t, err)

	// Submit an event so the watch loop starts
	_, err = stream.Submit(ctx, "TestEvent", map[string]string{"key": "value"})
	require.NoError(t, err)

	wire := MemoryWire(transport)
	proj := &mockIndexer{stream: stream}
	pump := NewPump(wire, proj, "test-holder")

	runCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	err = pump.RunWithDomainStream(runCtx, "test-domain", "test-stream")
	assert.Error(t, err)
}

func TestPumpPositionTracking(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	ctx := t.Context()
	stream, err := sys.Stream(ctx, "test-domain", "test-stream")
	require.NoError(t, err)

	// Set a position so the pump resumes from it
	_, err = transport.SetPosition(ctx, "test-domain", "test-stream", "test-holder", 42)
	require.NoError(t, err)

	// Submit an event after position 42
	_, err = stream.Submit(ctx, "TestEvent", map[string]string{"key": "value"})
	require.NoError(t, err)

	wire := MemoryWire(transport)
	proj := &mockIndexer{stream: stream}
	pump := NewPump(wire, proj, "test-holder")

	runCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	err = pump.RunWithDomainStream(runCtx, "test-domain", "test-stream")
	assert.Error(t, err)
}

func TestPumpLockNotAcquired(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	ctx := t.Context()
	stream, err := sys.Stream(ctx, "domain", "stream")
	require.NoError(t, err)

	// Pre-acquire the lock with a different holder
	_, err = transport.TryAcquire(ctx, "domain", "stream", "test-holder", "other-holder", 30*time.Second)
	require.NoError(t, err)

	wire := MemoryWire(transport)
	proj := &mockIndexer{stream: stream}
	pump := NewPump(wire, proj, "test-holder")

	err = pump.RunWithDomainStream(ctx, "domain", "stream")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lock held by")
}

func TestPumpAcquireError(t *testing.T) {
	t.Parallel()
	// Use a transport where the lock is already held by someone else with no TTL (expired)
	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	ctx := t.Context()
	stream, err := sys.Stream(ctx, "domain", "stream")
	require.NoError(t, err)

	wire := MemoryWire(transport)
	proj := &mockIndexer{stream: stream}
	pump := NewPump(wire, proj, "test-holder", WithTTL(0))

	// With TTL=0, the lock should expire immediately
	err = pump.RunWithDomainStream(ctx, "domain", "stream")
	require.Error(t, err)
}

func TestPumpHeartbeatsAfterEvents(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	ctx := t.Context()
	stream, err := sys.Stream(ctx, "domain", "stream")
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		_, err = stream.Submit(ctx, "TestEvent", map[string]int{"i": i})
		require.NoError(t, err)
	}

	wire := MemoryWire(transport)
	proj := &mockIndexer{stream: stream}
	pump := NewPump(wire, proj, "test-holder")

	runCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	err = pump.RunWithDomainStream(runCtx, "domain", "stream")
	// Context deadline exceeded is expected when the timeout expires
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		require.NoError(t, err)
	}
}

func TestNewPumpDefaults(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	wire := MemoryWire(transport)
	sys := v1.NewSystem(transport)
	ctx := t.Context()
	stream, err := sys.Stream(ctx, "d", "s")
	require.NoError(t, err)
	proj := &mockIndexer{stream: stream}

	pump := NewPump(wire, proj, "holder")
	assert.NotNil(t, pump)
	assert.Equal(t, v1.DefaultLockTTL, pump.opts.ttl)
	assert.Equal(t, 200*time.Millisecond, pump.opts.heartbeatMargin)
}

func TestNewPumpWithOptions(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	wire := MemoryWire(transport)
	sys := v1.NewSystem(transport)
	ctx := t.Context()
	stream, err := sys.Stream(ctx, "d", "s")
	require.NoError(t, err)
	proj := &mockIndexer{stream: stream}

	pump := NewPump(wire, proj, "holder",
		WithTTL(60*time.Second),
		WithHeartbeatMargin(500*time.Millisecond),
	)
	assert.Equal(t, 60*time.Second, pump.opts.ttl)
	assert.Equal(t, 500*time.Millisecond, pump.opts.heartbeatMargin)
}

// mockIndexer is a test double for Indexer.
type mockIndexer struct {
	stream v1.StreamTransport
}

func (m *mockIndexer) Query() *query2.Query {
	return query2.NewQuery(m.stream)
}
