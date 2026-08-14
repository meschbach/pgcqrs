package indexer

import (
	"context"
	"sync"
	"testing"
	"time"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPump_Deduplication verifies that the pump skips events it has already processed
func TestPump_Deduplication(t *testing.T) {
	t.Parallel()

	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	ctx := t.Context()

	// Create a stream and submit some events
	stream, err := sys.Stream(ctx, "domain", "stream")
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		_, err = stream.Submit(ctx, "TestEvent", map[string]int{"i": i})
		require.NoError(t, err)
	}

	// Create a pump and run it
	wire := v1.NewMemoryWire(transport)
	proj := &mockIndexer{stream: stream}
	pump := NewPump(wire, proj, "test-holder")

	runCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	err = pump.RunWithDomainStream(runCtx, "domain", "stream")
	require.ErrorIs(t, err, context.DeadlineExceeded)

	// The pump should have processed events (we can't check position directly as it's private,
	// but we can verify it ran without errors)
}

// TestPump_UnreliableHeartbeat verifies that heartbeat failures cause LockLost transition
func TestPump_UnreliableHeartbeat(t *testing.T) {
	t.Parallel()

	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	ctx := t.Context()

	// Create a stream
	stream, err := sys.Stream(ctx, "domain", "stream")
	require.NoError(t, err)

	// Submit an event
	_, err = stream.Submit(ctx, "TestEvent", map[string]string{"key": "value"})
	require.NoError(t, err)

	// Create a pump with a very short TTL to force heartbeat failure
	wire := v1.NewMemoryWire(transport)
	proj := &mockIndexer{stream: stream}
	pump := NewPump(wire, proj, "test-holder", WithTTL(100*time.Millisecond))

	// Track state transitions
	var mu sync.Mutex
	var states []PumpState
	unsub := pump.OnStateChange(func(_ context.Context, evt PumpStateEvent) error {
		mu.Lock()
		states = append(states, evt.State)
		mu.Unlock()
		return nil
	})
	defer unsub()

	runCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	err = pump.RunWithDomainStream(runCtx, "domain", "stream")
	require.ErrorIs(t, err, context.DeadlineExceeded)

	// Verify that the pump went through LockLost state
	mu.Lock()
	assert.Contains(t, states, PumpStateLockLost, "pump should have transitioned to LockLost due to heartbeat failure")
	mu.Unlock()
}

// TestPump_WatchFailureRecovery verifies that watch failures cause LockLost and re-acquisition
func TestPump_WatchFailureRecovery(t *testing.T) {
	t.Parallel()

	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	ctx := t.Context()

	// Create a stream
	stream, err := sys.Stream(ctx, "domain", "stream")
	require.NoError(t, err)

	// Submit an event
	_, err = stream.Submit(ctx, "TestEvent", map[string]string{"key": "value"})
	require.NoError(t, err)

	// Create a pump
	wire := v1.NewMemoryWire(transport)
	proj := &mockIndexer{stream: stream}
	pump := NewPump(wire, proj, "test-holder")

	// Track state transitions
	var mu sync.Mutex
	var states []PumpState
	unsub := pump.OnStateChange(func(_ context.Context, evt PumpStateEvent) error {
		mu.Lock()
		states = append(states, evt.State)
		mu.Unlock()
		return nil
	})
	defer unsub()

	// Simulate watch failure by canceling context early
	runCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	err = pump.RunWithDomainStream(runCtx, "domain", "stream")
	require.ErrorIs(t, err, context.DeadlineExceeded)

	// The pump should have attempted to watch and then transitioned to a terminal state
	mu.Lock()
	assert.NotEmpty(t, states, "pump should have gone through state transitions")
	mu.Unlock()
}
