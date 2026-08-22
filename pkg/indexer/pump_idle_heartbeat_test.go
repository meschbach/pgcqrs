package indexer

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"testing/synctest"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/meschbach/pgcqrs/pkg/v1/query2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// newRecordingIndexer creates a fresh indexer for each test
func newRecordingIndexer(stream v1.StreamTransport) *recordingIndexer {
	return &recordingIndexer{stream: stream}
}

// submitInitialEvent submits the first event to the stream.
func submitInitialEvent(t *testing.T, stream *v1.Stream) {
	_, err := stream.Submit(t.Context(), "TestEvent", map[string]string{"data": "initial"})
	require.NoError(t, err)
}

type heartbeatTestFunc func(t *testing.T, ctx context.Context, transport v1.Transport, stream *v1.Stream, pump *Pump[*v1.MemoryLock])

func runTest(t *testing.T, perform heartbeatTestFunc) {
	synctest.Test(t, func(t *testing.T) {
		transport := v1.NewMemoryTransport()
		t.Cleanup(func() {
			require.NoError(t, transport.Close())
		})

		sys := v1.NewSystem(transport)
		ctx := t.Context()

		stream, err := sys.Stream(ctx, "domain", "stream")
		require.NoError(t, err)

		submitInitialEvent(t, stream)

		wire := v1.NewMemoryWire(transport)
		indexer := newRecordingIndexer(stream)
		pump := NewPump[*v1.MemoryLock](wire, indexer, "test-holder", WithTTL(10*time.Second))

		perform(t, ctx, transport, stream, pump)
	})
}

// TestPump_IdleHeartbeat_HeartbeatKeepsLockAlive verifies that the heartbeat
// keeps the lock alive when the pump is idle for the full TTL period.
func TestPump_IdleHeartbeat_HeartbeatKeepsLockAlive(t *testing.T) {
	t.Parallel()
	runTest(t, func(t *testing.T, ctx context.Context, transport v1.Transport, _ *v1.Stream, pump *Pump[*v1.MemoryLock]) {
		pumpCtx, pumpCancel := context.WithCancel(ctx)
		t.Cleanup(pumpCancel)

		pumpDone := make(chan error, 1)
		go func() {
			pumpDone <- pump.RunWithDomainStream(pumpCtx, "domain", "stream")
		}()

		require.Eventually(t, func() bool {
			return pump.State() == PumpStateWatching
		}, 2*time.Second, 50*time.Millisecond)

		// Wait for idle period (longer than heartbeat interval)
		time.Sleep(10 * time.Second)

		// Verify pump is still in Watching state (not LockLost or Failed)
		assert.Equal(t, PumpStateWatching, pump.State())

		// Verify lock is still held
		lock, found, err := transport.GetLock(ctx, "domain", "stream", "test-holder")
		require.NoError(t, err)
		assert.True(t, found)
		assert.NotNil(t, lock)

		pumpCancel()
		<-pumpDone
	})
}

// TestPump_IdleHeartbeat_LockStolenThenReAcquired verifies that when the lock
// is externally released, the pump detects the loss, re-acquires, and returns
// to Watching state.
func TestPump_IdleHeartbeat_LockStolenThenReAcquired(t *testing.T) {
	t.Parallel()
	runTest(t, func(t *testing.T, ctx context.Context, transport v1.Transport, stream *v1.Stream, pump *Pump[*v1.MemoryLock]) {
		// Track state transitions
		var stateTransitions []PumpState
		unsub := pump.OnStateChange(func(_ context.Context, evt PumpStateEvent) error {
			stateTransitions = append(stateTransitions, evt.State)
			return nil
		})
		t.Cleanup(unsub)

		pumpCtx, pumpCancel := context.WithCancel(ctx)
		t.Cleanup(pumpCancel)

		pumpDone := make(chan error, 1)
		go func() {
			pumpDone <- pump.RunWithDomainStream(pumpCtx, "domain", "stream")
		}()

		require.Eventually(t, func() bool {
			return pump.State() == PumpStateWatching
		}, 2*time.Second, 50*time.Millisecond)

		// Wait for idle period
		time.Sleep(7 * time.Second)

		// Externally release the lock (simulates theft)
		err := transport.Release(ctx, "domain", "stream", "test-holder", "test-holder")
		require.NoError(t, err)

		// Submit another event to trigger heartbeat
		_, err = stream.Submit(ctx, "TestEvent", map[string]string{"data": "after-theft"})
		require.NoError(t, err)

		// Wait for pump to detect lock loss and re-acquire
		require.Eventually(t, func() bool {
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

		pumpCancel()
		<-pumpDone
	})
}

// TestPump_IdleHeartbeat_IdleThenEventProcessed verifies that after an idle
// period, submitting a new event advances the position and the pump remains
// in Watching state.
func TestPump_IdleHeartbeat_IdleThenEventProcessed(t *testing.T) {
	t.Parallel()
	runTest(t, func(t *testing.T, ctx context.Context, transport v1.Transport, stream *v1.Stream, pump *Pump[*v1.MemoryLock]) {
		pumpCtx, pumpCancel := context.WithCancel(ctx)
		defer pumpCancel()

		pumpDone := make(chan error, 1)
		go func() {
			pumpDone <- pump.RunWithDomainStream(pumpCtx, "domain", "stream")
		}()

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

		pumpCancel()
		<-pumpDone
	})
}

// TestPump_IdleHeartbeat_ProactiveHeartbeatAtTTL09 verifies that a proactive
// heartbeat at TTL*0.9 keeps the lock alive when waiting slightly longer than
// the proactive interval.
func TestPump_IdleHeartbeat_ProactiveHeartbeatAtTTL09(t *testing.T) {
	t.Parallel()
	runTest(t, func(t *testing.T, ctx context.Context, transport v1.Transport, _ *v1.Stream, pump *Pump[*v1.MemoryLock]) {
		pumpCtx, pumpCancel := context.WithCancel(ctx)
		defer pumpCancel()

		pumpDone := make(chan error, 1)
		go func() {
			pumpDone <- pump.RunWithDomainStream(pumpCtx, "domain", "stream")
		}()

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

		pumpCancel()
		<-pumpDone
	})
}
