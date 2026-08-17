package indexer

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/meschbach/pgcqrs/pkg/v1/query2"
)

// recordingIndexer tracks which events it processes
type recordingIndexer struct {
	stream v1.StreamTransport
	mu     sync.Mutex
	seen   []int64
}

func (r *recordingIndexer) Query() *query2.Query {
	q := query2.NewQuery(r.stream)
	q.OnKind("TestEvent").Each(func(_ context.Context, e v1.Envelope, _ json.RawMessage) error {
		r.mu.Lock()
		r.seen = append(r.seen, e.ID)
		r.mu.Unlock()
		return nil
	})
	return q
}

func (r *recordingIndexer) getSeen() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]int64, len(r.seen))
	copy(result, r.seen)
	return result
}

func TestPump_IdleHeartbeat(t *testing.T) {
	t.Parallel()

	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	ctx := t.Context()

	stream, err := sys.Stream(ctx, "domain", "stream")
	require.NoError(t, err)

	// Submit one event
	_, err = stream.Submit(ctx, "TestEvent", map[string]string{"data": "initial"})
	require.NoError(t, err)

	// Create pump with short TTL for testing
	wire := v1.NewMemoryWire(transport)
	indexer := &recordingIndexer{stream: stream}
	pump := NewPump(wire, indexer, "test-holder", WithTTL(10*time.Second))

	// Start pump in background
	pumpCtx, pumpCancel := context.WithCancel(ctx)
	defer pumpCancel()

	pumpDone := make(chan error, 1)
	go func() {
		pumpDone <- pump.RunWithDomainStream(pumpCtx, "domain", "stream")
	}()

	// Wait for pump to reach Watching state
	require.Eventually(t, func() bool {
		return pump.State() == PumpStateWatching
	}, 2*time.Second, 50*time.Millisecond)

	// Wait for idle period (longer than heartbeat interval of 9s = 10s * 0.9)
	time.Sleep(10 * time.Second)

	// Verify pump is still in Watching state (not LockLost or Failed)
	assert.Equal(t, PumpStateWatching, pump.State())

	// Verify lock is still held
	lock, found, err := transport.GetLock(ctx, "domain", "stream", "test-holder")
	require.NoError(t, err)
	assert.True(t, found)
	assert.NotNil(t, lock)

	// Cleanup
	pumpCancel()
	<-pumpDone
}

func TestPump_IdleLockStolen(t *testing.T) {
	t.Parallel()

	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	ctx := t.Context()

	stream, err := sys.Stream(ctx, "domain", "stream")
	require.NoError(t, err)

	// Submit one event
	_, err = stream.Submit(ctx, "TestEvent", map[string]string{"data": "initial"})
	require.NoError(t, err)

	// Create pump with short TTL for testing
	wire := v1.NewMemoryWire(transport)
	indexer := &recordingIndexer{stream: stream}
	pump := NewPump(wire, indexer, "test-holder", WithTTL(7*time.Second))

	// Track state transitions
	var stateTransitions []PumpState
	unsub := pump.OnStateChange(func(_ context.Context, evt PumpStateEvent) error {
		stateTransitions = append(stateTransitions, evt.State)
		return nil
	})
	defer unsub()

	// Start pump in background
	pumpCtx, pumpCancel := context.WithCancel(ctx)
	defer pumpCancel()

	pumpDone := make(chan error, 1)
	go func() {
		pumpDone <- pump.RunWithDomainStream(pumpCtx, "domain", "stream")
	}()

	// Wait for pump to reach Watching state
	require.Eventually(t, func() bool {
		return pump.State() == PumpStateWatching
	}, 2*time.Second, 50*time.Millisecond)

	// Wait for idle period (longer than heartbeat interval of 6.3s = 7s * 0.9)
	time.Sleep(7 * time.Second)

	// Externally release the lock (simulates theft)
	err = transport.Release(ctx, "domain", "stream", "test-holder", "test-holder")
	require.NoError(t, err)

	// Submit another event to trigger heartbeat
	_, err = stream.Submit(ctx, "TestEvent", map[string]string{"data": "after-theft"})
	require.NoError(t, err)

	// Wait for pump to detect lock loss and re-acquire
	require.Eventually(t, func() bool {
		// Check if we've seen LockLost and then Acquiring
		hasLockLost := false
		hasAcquiring := false
		for _, state := range stateTransitions {
			if state == PumpStateLockLost {
				hasLockLost = true
			}
			if hasLockLost && state == PumpStateAcquiring {
				hasAcquiring = true
			}
		}
		return hasLockLost && hasAcquiring
	}, 5*time.Second, 100*time.Millisecond)

	// Wait for pump to reach Watching state again
	require.Eventually(t, func() bool {
		return pump.State() == PumpStateWatching
	}, 3*time.Second, 100*time.Millisecond)

	// Cleanup
	pumpCancel()
	<-pumpDone
}

func TestPump_IdleThenEvent(t *testing.T) {
	t.Parallel()

	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	ctx := t.Context()

	stream, err := sys.Stream(ctx, "domain", "stream")
	require.NoError(t, err)

	// Submit first event
	_, err = stream.Submit(ctx, "TestEvent", map[string]string{"data": "event1"})
	require.NoError(t, err)

	// Create pump with short TTL for testing
	wire := v1.NewMemoryWire(transport)
	indexer := &recordingIndexer{stream: stream}
	pump := NewPump(wire, indexer, "test-holder", WithTTL(10*time.Second))

	// Start pump in background
	pumpCtx, pumpCancel := context.WithCancel(ctx)
	defer pumpCancel()

	pumpDone := make(chan error, 1)
	go func() {
		pumpDone <- pump.RunWithDomainStream(pumpCtx, "domain", "stream")
	}()

	// Wait for pump to reach Watching state
	require.Eventually(t, func() bool {
		return pump.State() == PumpStateWatching
	}, 2*time.Second, 50*time.Millisecond)

	// Get initial position
	position1, _, err := transport.GetPosition(ctx, "domain", "stream", "test-holder")
	require.NoError(t, err)

	// Wait for idle period (longer than heartbeat interval of 9s = 10s * 0.9)
	time.Sleep(10 * time.Second)

	// Submit second event
	_, err = stream.Submit(ctx, "TestEvent", map[string]string{"data": "event2"})
	require.NoError(t, err)

	// Wait for position to advance
	require.Eventually(t, func() bool {
		position2, _, err := transport.GetPosition(ctx, "domain", "stream", "test-holder")
		require.NoError(t, err)
		return position2 > position1
	}, 3*time.Second, 100*time.Millisecond)

	// Verify position advanced (event was processed)
	position2, _, err := transport.GetPosition(ctx, "domain", "stream", "test-holder")
	require.NoError(t, err)
	assert.Greater(t, position2, position1)

	// Verify pump is still in Watching state
	assert.Equal(t, PumpStateWatching, pump.State())

	// Cleanup
	pumpCancel()
	<-pumpDone
}

func TestPump_HeartbeatIntervalFromServer(t *testing.T) {
	t.Parallel()

	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	ctx := t.Context()

	stream, err := sys.Stream(ctx, "domain", "stream")
	require.NoError(t, err)

	// Submit one event
	_, err = stream.Submit(ctx, "TestEvent", map[string]string{"data": "initial"})
	require.NoError(t, err)

	// Create pump with specific TTL
	ttl := 20 * time.Second
	wire := v1.NewMemoryWire(transport)
	indexer := &recordingIndexer{stream: stream}
	pump := NewPump(wire, indexer, "test-holder", WithTTL(ttl))

	// Start pump in background
	pumpCtx, pumpCancel := context.WithCancel(ctx)
	defer pumpCancel()

	pumpDone := make(chan error, 1)
	go func() {
		pumpDone <- pump.RunWithDomainStream(pumpCtx, "domain", "stream")
	}()

	// Wait for pump to reach Watching state
	require.Eventually(t, func() bool {
		return pump.State() == PumpStateWatching
	}, 2*time.Second, 50*time.Millisecond)

	// Expected heartbeat interval is TTL * 0.9 = 18s
	// Watch timeout is heartbeat interval * 0.9 = 16.2s
	// So proactive heartbeat should happen around 16.2s

	// Wait for 17 seconds (should trigger proactive heartbeat)
	time.Sleep(17 * time.Second)

	// Verify pump is still in Watching state
	assert.Equal(t, PumpStateWatching, pump.State())

	// Verify lock is still held
	lock, found, err := transport.GetLock(ctx, "domain", "stream", "test-holder")
	require.NoError(t, err)
	assert.True(t, found)
	assert.NotNil(t, lock)

	// Cleanup
	pumpCancel()
	<-pumpDone
}
