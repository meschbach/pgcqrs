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

	stream, err := sys.Stream(ctx, "domain", "stream")
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		_, err = stream.Submit(ctx, "TestEvent", map[string]int{"i": i})
		require.NoError(t, err)
	}

	wire := v1.NewMemoryWire(transport)
	indexer := &recordingIndexer{stream: stream}
	pump := NewPump(wire, indexer, "test-holder")

	pumpCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- pump.RunWithDomainStream(pumpCtx, "domain", "stream")
	}()

	require.Eventually(t, func() bool {
		return pump.State() == PumpStateWatching
	}, 2*time.Second, 50*time.Millisecond)

	cancel()
	<-done

	seen := indexer.getSeen()
	require.Len(t, seen, 3, "all 3 events should be processed exactly once")

	// Verify events were processed in order
	for i := 1; i < len(seen); i++ {
		assert.Greater(t, seen[i], seen[i-1], "events should be processed in ascending ID order")
	}
}

// TestPump_UnreliableHeartbeat verifies lock-loss recovery: the pump detects
// when its lock is externally released and re-acquires automatically.
func TestPump_UnreliableHeartbeat(t *testing.T) {
	t.Parallel()

	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	ctx := t.Context()

	stream, err := sys.Stream(ctx, "domain", "stream")
	require.NoError(t, err)

	_, err = stream.Submit(ctx, "TestEvent", map[string]string{"key": "value"})
	require.NoError(t, err)

	wire := v1.NewMemoryWire(transport)
	indexer := &recordingIndexer{stream: stream}
	pump := NewPump(wire, indexer, "test-holder", WithTTL(10*time.Second))

	var mu sync.Mutex
	var states []PumpState
	unsub := pump.OnStateChange(func(_ context.Context, evt PumpStateEvent) error {
		mu.Lock()
		states = append(states, evt.State)
		mu.Unlock()
		return nil
	})
	defer unsub()

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

	// Externally release the lock (simulates heartbeat failure)
	err = transport.Release(ctx, "domain", "stream", "test-holder", "test-holder")
	require.NoError(t, err)

	// Submit another event to trigger a heartbeat attempt
	_, err = stream.Submit(ctx, "TestEvent", map[string]string{"data": "after-release"})
	require.NoError(t, err)

	// Wait for pump to detect lock loss and re-acquire
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		hasLockLost := false
		hasAcquiring := false
		for _, state := range states {
			if state == PumpStateLockLost {
				hasLockLost = true
			}
			if hasLockLost && state == PumpStateAcquiring {
				hasAcquiring = true
			}
		}
		return hasLockLost && hasAcquiring
	}, 5*time.Second, 100*time.Millisecond, "pump should transition to LockLost then Acquiring after external release")

	// Wait for pump to re-acquire and reach Watching state again
	require.Eventually(t, func() bool {
		return pump.State() == PumpStateWatching
	}, 3*time.Second, 100*time.Millisecond)

	pumpCancel()
	<-pumpDone
}

// TestPump_WatchFailureRecovery verifies that the pump exits cleanly on context
// cancellation while in the Watching state.
func TestPump_WatchFailureRecovery(t *testing.T) {
	t.Parallel()

	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	ctx := t.Context()

	stream, err := sys.Stream(ctx, "domain", "stream")
	require.NoError(t, err)

	_, err = stream.Submit(ctx, "TestEvent", map[string]string{"key": "value"})
	require.NoError(t, err)

	wire := v1.NewMemoryWire(transport)
	indexer := &recordingIndexer{stream: stream}
	pump := NewPump(wire, indexer, "test-holder")

	var mu sync.Mutex
	var states []PumpState
	unsub := pump.OnStateChange(func(_ context.Context, evt PumpStateEvent) error {
		mu.Lock()
		states = append(states, evt.State)
		mu.Unlock()
		return nil
	})
	defer unsub()

	pumpCtx, pumpCancel := context.WithCancel(ctx)
	defer pumpCancel()

	done := make(chan error, 1)
	go func() {
		done <- pump.RunWithDomainStream(pumpCtx, "domain", "stream")
	}()

	// Wait for pump to reach Watching state
	require.Eventually(t, func() bool {
		return pump.State() == PumpStateWatching
	}, 2*time.Second, 50*time.Millisecond)

	// Cancel context to trigger clean shutdown
	pumpCancel()
	err = <-done
	require.ErrorIs(t, err, context.Canceled)

	mu.Lock()
	assert.NotEmpty(t, states, "pump should have gone through state transitions")
	mu.Unlock()
}

// TestPump_HeartbeatConflictUpdatesPosition verifies that when a heartbeat
// returns HeartbeatConflictError (position behind server), the pump adopts
// the server's position and continues processing without lock loss.
func TestPump_HeartbeatConflictUpdatesPosition(t *testing.T) {
	t.Parallel()

	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	ctx := t.Context()

	stream, err := sys.Stream(ctx, "domain", "stream")
	require.NoError(t, err)

	// Submit 2 events
	for i := 0; i < 2; i++ {
		_, err = stream.Submit(ctx, "TestEvent", map[string]string{"key": "value"})
		require.NoError(t, err)
	}

	wire := v1.NewMemoryWire(transport)
	indexer := &recordingIndexer{stream: stream}
	pump := NewPump(wire, indexer, "test-holder", WithTTL(10*time.Second))

	var mu sync.Mutex
	var states []PumpState
	unsub := pump.OnStateChange(func(_ context.Context, evt PumpStateEvent) error {
		mu.Lock()
		states = append(states, evt.State)
		mu.Unlock()
		return nil
	})
	defer unsub()

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

	// Wait for first event to be processed
	require.Eventually(t, func() bool {
		return len(indexer.getSeen()) >= 1
	}, 3*time.Second, 50*time.Millisecond)

	// Externally advance the position to simulate another consumer
	// advancing the position. The pump's next heartbeat will carry a stale
	// position, triggering HeartbeatConflictError.
	_, err = transport.SetPosition(ctx, "domain", "stream", "test-holder", 100)
	require.NoError(t, err)

	// Submit another event to trigger a heartbeat with stale position
	_, err = stream.Submit(ctx, "TestEvent", map[string]string{"key": "after-conflict"})
	require.NoError(t, err)

	// Wait for pump to process the event and handle the conflict
	require.Eventually(t, func() bool {
		return len(indexer.getSeen()) >= 2
	}, 5*time.Second, 50*time.Millisecond)

	// Pump should still be running (conflict was self-healed)
	assert.Equal(t, PumpStateWatching, pump.State())

	// Position should have been updated to the conflict version
	pos, found, err := transport.GetPosition(ctx, "domain", "stream", "test-holder")
	require.NoError(t, err)
	assert.True(t, found)
	assert.GreaterOrEqual(t, pos, int64(100), "position should be at least the conflict version")

	// No lock loss should have occurred — conflict was handled inline
	mu.Lock()
	for _, state := range states {
		assert.NotEqual(t, PumpStateLockLost, state, "heartbeat conflict should not cause lock loss")
	}
	mu.Unlock()

	pumpCancel()
	<-pumpDone
}

// heartbeatFailingLock wraps a MemoryLock and can be configured to fail heartbeats.
type heartbeatFailingLock struct {
	*v1.MemoryLock
	failHeartbeat bool
}

func (l *heartbeatFailingLock) Heartbeat(ctx context.Context, position int64) error {
	if l.failHeartbeat {
		return &v1.LockNotHeldError{Consumer: "consumer", Holder: "holder", Domain: "domain", Stream: "stream"}
	}
	return l.MemoryLock.Heartbeat(ctx, position)
}

// heartbeatFailingWire wraps a MemoryWire and returns locks that can fail heartbeats.
type heartbeatFailingWire struct {
	*v1.MemoryWire
	locks []*heartbeatFailingLock
}

func (w *heartbeatFailingWire) WaitForLock(ctx context.Context, domain, stream, consumer, holder string, ttl time.Duration) (Lock, int64, time.Duration, error) {
	lock, pos, interval, err := w.MemoryWire.WaitForLock(ctx, domain, stream, consumer, holder, ttl)
	if err != nil {
		return nil, 0, 0, err
	}
	fl := &heartbeatFailingLock{MemoryLock: lock}
	w.locks = append(w.locks, fl)
	return fl, pos, interval, nil
}

func (w *heartbeatFailingWire) setFailHeartbeat(fail bool) {
	for _, l := range w.locks {
		l.failHeartbeat = fail
	}
}

// TestPump_HeartbeatFailureRecovery verifies that when heartbeat RPCs fail,
// the pump transitions to LockLost and re-acquires the lock.
func TestPump_HeartbeatFailureRecovery(t *testing.T) {
	t.Parallel()

	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	ctx := t.Context()

	stream, err := sys.Stream(ctx, "domain", "stream")
	require.NoError(t, err)

	_, err = stream.Submit(ctx, "TestEvent", map[string]string{"key": "value"})
	require.NoError(t, err)

	baseWire := v1.NewMemoryWire(transport)
	wire := &heartbeatFailingWire{MemoryWire: baseWire}
	indexer := &recordingIndexer{stream: stream}
	pump := NewPump(wire, indexer, "test-holder", WithTTL(10*time.Second))

	var mu sync.Mutex
	var states []PumpState
	unsub := pump.OnStateChange(func(_ context.Context, evt PumpStateEvent) error {
		mu.Lock()
		states = append(states, evt.State)
		mu.Unlock()
		return nil
	})
	defer unsub()

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

	// Configure the lock to fail heartbeats
	wire.setFailHeartbeat(true)

	// Submit another event to trigger a heartbeat attempt
	_, err = stream.Submit(ctx, "TestEvent", map[string]string{"data": "after-failure"})
	require.NoError(t, err)

	// Wait for pump to detect heartbeat failure and transition to LockLost
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, state := range states {
			if state == PumpStateLockLost {
				return true
			}
		}
		return false
	}, 5*time.Second, 100*time.Millisecond, "pump should transition to LockLost after heartbeat failure")

	// Re-enable heartbeats and wait for pump to re-acquire
	wire.setFailHeartbeat(false)
	require.Eventually(t, func() bool {
		return pump.State() == PumpStateWatching
	}, 3*time.Second, 100*time.Millisecond)

	pumpCancel()
	<-pumpDone
}
