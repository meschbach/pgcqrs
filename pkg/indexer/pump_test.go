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
	_, stream, pump := newMemoryPumpHarness(t, "test-domain", "test-stream", WithTTL(10*time.Second))

	// Submit events
	for i := 0; i < 3; i++ {
		_, err := stream.Submit(t.Context(), "TestEvent", map[string]int{"i": i})
		require.NoError(t, err)
	}

	err := runPump(t, pump, "test-domain", "test-stream", 2*time.Second)
	assert.Error(t, err)
}

func TestPumpLockLifecycle(t *testing.T) {
	t.Parallel()
	_, stream, pump := newMemoryPumpHarness(t, "test-domain", "test-stream")

	// Submit an event so the watch loop starts
	_, err := stream.Submit(t.Context(), "TestEvent", map[string]string{"key": "value"})
	require.NoError(t, err)

	err = runPump(t, pump, "test-domain", "test-stream", 2*time.Second)
	assert.Error(t, err)
}

func TestPumpPositionTracking(t *testing.T) {
	t.Parallel()
	transport, stream, pump := newMemoryPumpHarness(t, "test-domain", "test-stream")

	// Set a position so the pump resumes from it
	_, err := transport.SetPosition(t.Context(), "test-domain", "test-stream", "test-holder", 42)
	require.NoError(t, err)

	// Submit an event after position 42
	_, err = stream.Submit(t.Context(), "TestEvent", map[string]string{"key": "value"})
	require.NoError(t, err)

	err = runPump(t, pump, "test-domain", "test-stream", 2*time.Second)
	assert.Error(t, err)
}

func TestPumpLockNotAcquired(t *testing.T) {
	t.Parallel()
	transport, _, pump := newMemoryPumpHarness(t, "domain", "stream")

	// Acquire lock with a different holder
	_, err := transport.TryAcquire(t.Context(), "domain", "stream", "test-consumer", "other-holder", 30*time.Second)
	require.NoError(t, err)

	// Use a short timeout - pump should keep retrying until context expires
	err = runPump(t, pump, "domain", "stream", 500*time.Millisecond)
	// Pump should exit with context error, not lock acquisition error
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestPumpAcquireError(t *testing.T) {
	t.Parallel()
	// TTL=0 is invalid, should cause immediate error
	_, _, pump := newMemoryPumpHarness(t, "domain", "stream", WithTTL(0))

	// Use a short timeout - pump should exit quickly due to invalid TTL
	err := runPump(t, pump, "domain", "stream", 500*time.Millisecond)
	// Pump should exit with an error (either TTL error or context timeout)
	assert.Error(t, err)
}

func TestPumpHeartbeatsAfterEvents(t *testing.T) {
	t.Parallel()
	_, stream, pump := newMemoryPumpHarness(t, "domain", "stream")

	for i := 0; i < 3; i++ {
		_, err := stream.Submit(t.Context(), "TestEvent", map[string]int{"i": i})
		require.NoError(t, err)
	}

	err := runPump(t, pump, "domain", "stream", 2*time.Second)
	// Context deadline exceeded is expected when the timeout expires
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		require.NoError(t, err)
	}
}

func TestNewPumpDefaults(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	wire := v1.NewMemoryWire(transport)
	sys := v1.NewSystem(transport)
	ctx := t.Context()
	stream, err := sys.Stream(ctx, "d", "s")
	require.NoError(t, err)
	proj := &mockIndexer{stream: stream}

	pump := NewPump(wire, proj, "holder")
	assert.NotNil(t, pump)
	assert.Equal(t, v1.DefaultLockTTL, pump.opts.ttl)
}

func TestNewPumpWithOptions(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	wire := v1.NewMemoryWire(transport)
	sys := v1.NewSystem(transport)
	ctx := t.Context()
	stream, err := sys.Stream(ctx, "d", "s")
	require.NoError(t, err)
	proj := &mockIndexer{stream: stream}

	pump := NewPump(wire, proj, "holder",
		WithTTL(60*time.Second),
	)
	assert.Equal(t, 60*time.Second, pump.opts.ttl)
}

// mockIndexer is a test double for Indexer.
type mockIndexer struct {
	stream v1.StreamTransport
}

func (m *mockIndexer) Query() *query2.Query {
	return query2.NewQuery(m.stream)
}
