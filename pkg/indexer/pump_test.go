package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/meschbach/pgcqrs/pkg/v1/query2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPumpAcquiresLockAndProcessesEvents(t *testing.T) {
	t.Parallel()
	transport, stream, pump, indexer := newRecordingPumpHarness(t, "test-domain", "test-stream", WithTTL(10*time.Second))

	for i := 0; i < 3; i++ {
		_, err := stream.Submit(t.Context(), "TestEvent", map[string]int{"i": i})
		require.NoError(t, err)
	}

	pumpCtx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- pump.RunWithDomainStream(pumpCtx, "test-domain", "test-stream")
	}()

	require.Eventually(t, func() bool {
		return pump.State() == PumpStateWatching
	}, 2*time.Second, 50*time.Millisecond)

	require.Eventually(t, func() bool {
		return len(indexer.getSeen()) == 3
	}, 2*time.Second, 50*time.Millisecond, "all 3 events should be processed")

	cancel()
	<-done

	seen := indexer.getSeen()
	assert.Len(t, seen, 3, "all 3 events should be processed")

	pos, found, err := transport.GetPosition(t.Context(), "test-domain", "test-stream", "test-holder")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Positive(t, pos, "position should advance after processing events")
}

func TestPumpLockLifecycle(t *testing.T) {
	t.Parallel()
	transport, stream, pump := newMemoryPumpHarness(t, "test-domain", "test-stream", WithTTL(10*time.Second))

	_, err := stream.Submit(t.Context(), "TestEvent", map[string]string{"key": "value"})
	require.NoError(t, err)

	pumpCtx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- pump.RunWithDomainStream(pumpCtx, "test-domain", "test-stream")
	}()

	require.Eventually(t, func() bool {
		return pump.State() == PumpStateWatching
	}, 2*time.Second, 50*time.Millisecond)

	lock, found, err := transport.GetLock(t.Context(), "test-domain", "test-stream", "test-holder")
	require.NoError(t, err)
	assert.True(t, found)
	assert.NotNil(t, lock)

	cancel()
	<-done
}

func TestPumpPositionTracking(t *testing.T) {
	t.Parallel()
	transport, stream, pump, indexer := newRecordingPumpHarness(t, "test-domain", "test-stream", WithTTL(10*time.Second))

	// Set position to 1 so the pump skips events with ID <= 1 (events 0 and 1)
	_, err := transport.SetPosition(t.Context(), "test-domain", "test-stream", "test-holder", 1)
	require.NoError(t, err)

	// Submit 5 events (IDs 0-4); pump should only process IDs 2, 3, 4
	for i := 0; i < 5; i++ {
		_, err = stream.Submit(t.Context(), "TestEvent", map[string]string{"key": "value"})
		require.NoError(t, err)
	}

	pumpCtx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- pump.RunWithDomainStream(pumpCtx, "test-domain", "test-stream")
	}()

	require.Eventually(t, func() bool {
		return pump.State() == PumpStateWatching
	}, 2*time.Second, 50*time.Millisecond)

	// Wait for all 3 events to be processed
	require.Eventually(t, func() bool {
		return len(indexer.getSeen()) >= 3
	}, 5*time.Second, 50*time.Millisecond)

	cancel()
	<-done

	seen := indexer.getSeen()
	require.Len(t, seen, 3, "only events after position 1 should be processed")
	assert.Equal(t, int64(2), seen[0], "first processed event should be ID 2")
	assert.Equal(t, int64(3), seen[1], "second processed event should be ID 3")
	assert.Equal(t, int64(4), seen[2], "third processed event should be ID 4")

	pos, found, err := transport.GetPosition(t.Context(), "test-domain", "test-stream", "test-holder")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Greater(t, pos, int64(1), "position should advance past initial value")
}

func TestPumpLockNotAcquired(t *testing.T) {
	t.Parallel()
	transport, _, pump := newMemoryPumpHarness(t, "domain", "stream")

	_, err := transport.TryAcquire(t.Context(), "domain", "stream", "test-consumer", "other-holder", 30*time.Second)
	require.NoError(t, err)

	err = runPump(t, pump, "domain", "stream", 500*time.Millisecond)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestPumpAcquireError(t *testing.T) {
	t.Parallel()
	_, _, pump := newMemoryPumpHarness(t, "domain", "stream", WithTTL(0))

	err := runPump(t, pump, "domain", "stream", 500*time.Millisecond)
	assert.Error(t, err)
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

// mockIndexer is a test double for Indexer with a minimal query.
// Events submitted to the stream will NOT be delivered to the pump.
// Use recordingIndexer for tests that need events to be processed.
type mockIndexer struct {
	stream v1.StreamTransport
}

func (m *mockIndexer) Query() *query2.Query {
	q := query2.NewQuery(m.stream)
	q.OnKind("TestEvent").Each(func(_ context.Context, _ v1.Envelope, _ json.RawMessage) error {
		return nil
	})
	return q
}

// failingIndexer is a test double whose handler always returns an error.
// Used to verify that handler errors transition the pump to PumpStateFailed.
type failingIndexer struct {
	stream v1.StreamTransport
	err    error
}

func (f *failingIndexer) Query() *query2.Query {
	q := query2.NewQuery(f.stream)
	q.OnKind("TestEvent").Each(func(_ context.Context, _ v1.Envelope, _ json.RawMessage) error {
		return f.err
	})
	return q
}

func TestPump_HandlerErrorTransitionsToFailed(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	ctx := t.Context()
	stream, err := sys.Stream(ctx, "domain", "stream")
	require.NoError(t, err)

	_, err = stream.Submit(ctx, "TestEvent", map[string]string{"key": "value"})
	require.NoError(t, err)

	wire := v1.NewMemoryWire(transport)
	indexer := &failingIndexer{stream: stream, err: errors.New("indexing failed")}
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

	runErr := pump.RunWithDomainStream(ctx, "domain", "stream")

	// Pump should have entered Failed state
	assert.Equal(t, PumpStateFailed, pump.State())

	// Verify the state sequence included Failed
	mu.Lock()
	assert.Contains(t, states, PumpStateFailed)
	mu.Unlock()

	// Pump should have exited with an error (not context cancellation)
	require.Error(t, runErr)
	assert.NotErrorIs(t, runErr, context.Canceled, "should exit with application error, not context cancel")
}
