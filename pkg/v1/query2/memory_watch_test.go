package query2

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type watchTestEvent struct {
	ID    string `json:"id"`
	Value string `json:"value,omitempty"`
}

func TestMemoryQueryWatchNoDuplicateDelivery(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	transport := v1.NewMemoryTransport()
	system := v1.NewSystem(transport)
	domain := "test-domain"
	streamName := "test-stream"
	stream := system.MustStream(ctx, domain, streamName)

	// Submit3 events:1 Created +2 Updated
	stream.MustSubmit(ctx, "Created", watchTestEvent{ID: "item-1"})
	stream.MustSubmit(ctx, "Updated", watchTestEvent{ID: "item-1"})
	stream.MustSubmit(ctx, "Updated", watchTestEvent{ID: "item-1"})

	// Build a query with per-kind counters
	var mu sync.Mutex
	counts := make(map[string]int)

	q := NewQuery(stream)
	q.OnKind("Created").Each(func(_ context.Context, _ v1.Envelope, _ json.RawMessage) error {
		mu.Lock()
		counts["Created"]++
		mu.Unlock()
		return nil
	})
	q.OnKind("Updated").Each(func(_ context.Context, _ v1.Envelope, _ json.RawMessage) error {
		mu.Lock()
		counts["Updated"]++
		mu.Unlock()
		return nil
	})

	watch, err := q.Watch(ctx)
	require.NoError(t, err)

	// Process all expected events
	for range 3 {
		_, err := watch.TickWithID(ctx)
		require.NoError(t, err)
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, counts["Created"], "Created handler should fire exactly once")
	assert.Equal(t, 2, counts["Updated"], "Updated handler should fire exactly twice")
}

func TestMemoryQueryWatchNoDuplicateRapidSubmit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	transport := v1.NewMemoryTransport()
	system := v1.NewSystem(transport)
	domain := "test-domain"
	streamName := "test-stream"
	stream := system.MustStream(ctx, domain, streamName)

	// Pre-submit all events before watching
	const eventCount = 20
	for range eventCount {
		stream.MustSubmit(ctx, "items", watchTestEvent{ID: "item"})
	}

	// Build query
	var mu sync.Mutex
	count := 0

	q := NewQuery(stream)
	q.OnKind("items").Each(func(_ context.Context, _ v1.Envelope, _ json.RawMessage) error {
		mu.Lock()
		count++
		mu.Unlock()
		return nil
	})

	watch, err := q.Watch(ctx)
	require.NoError(t, err)

	// Process all events
	for range eventCount {
		_, err := watch.TickWithID(ctx)
		require.NoError(t, err)
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, eventCount, count, "each event should be delivered exactly once")
}

func TestMemoryWatchDeliversFirstPostInitEvent(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	transport := v1.NewMemoryTransport()
	system := v1.NewSystem(transport)
	stream := system.MustStream(ctx, "test-domain", "test-stream")

	// Pre-submit 3 events that will be init events
	stream.MustSubmit(ctx, "items", watchTestEvent{ID: "pre-1"})
	stream.MustSubmit(ctx, "items", watchTestEvent{ID: "pre-2"})
	stream.MustSubmit(ctx, "items", watchTestEvent{ID: "pre-3"})

	var mu sync.Mutex
	var receivedCount int

	q := NewQuery(stream)
	q.OnKind("items").Each(func(_ context.Context, _ v1.Envelope, _ json.RawMessage) error {
		mu.Lock()
		receivedCount++
		mu.Unlock()
		return nil
	})
	watch, err := q.Watch(ctx)
	require.NoError(t, err)

	// Consume all init events
	for i := 0; i < 3; i++ {
		_, err := watch.TickWithID(ctx)
		require.NoError(t, err)
	}

	mu.Lock()
	require.Equal(t, 3, receivedCount, "should have delivered 3 init events")
	mu.Unlock()

	// Submit a new event after init events are consumed
	sub := stream.MustSubmit(ctx, "items", watchTestEvent{ID: "post-1"})

	// Tick should deliver the post-init event
	id, err := watch.TickWithID(ctx)
	require.NoError(t, err)
	assert.Equal(t, sub.ID, id)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 4, receivedCount, "should have delivered 4 events total")
}

func TestMemoryWatchDeduplicatesInitEvents(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	transport := v1.NewMemoryTransport()
	system := v1.NewSystem(transport)
	stream := system.MustStream(ctx, "test-domain", "test-stream")

	stream.MustSubmit(ctx, "items", watchTestEvent{ID: "a"})
	stream.MustSubmit(ctx, "items", watchTestEvent{ID: "b"})

	var mu sync.Mutex
	count := 0

	q := NewQuery(stream)
	q.OnKind("items").Each(func(_ context.Context, _ v1.Envelope, _ json.RawMessage) error {
		mu.Lock()
		count++
		mu.Unlock()
		return nil
	})
	watch, err := q.Watch(ctx)
	require.NoError(t, err)

	// Consume both init events
	for i := 0; i < 2; i++ {
		_, err := watch.TickWithID(ctx)
		require.NoError(t, err)
	}

	mu.Lock()
	assert.Equal(t, 2, count, "should have delivered exactly 2 init events")
	mu.Unlock()

	// A subsequent Tick with no new events should block until context expires
	shortCtx, shortCancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer shortCancel()
	_, tickErr := watch.TickWithID(shortCtx)
	require.ErrorIs(t, tickErr, context.DeadlineExceeded)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 2, count, "no duplicate events should have been delivered")
}

func TestMemoryWatchNewEventsOnEmptyStream(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	transport := v1.NewMemoryTransport()
	system := v1.NewSystem(transport)
	stream := system.MustStream(ctx, "test-domain", "test-stream")

	// Create watch on an empty stream (no pre-existing events)
	var mu sync.Mutex
	var receivedIDs []int64

	q := NewQuery(stream)
	q.OnKind("items").Each(func(_ context.Context, e v1.Envelope, _ json.RawMessage) error {
		mu.Lock()
		receivedIDs = append(receivedIDs, e.ID)
		mu.Unlock()
		return nil
	})
	watch, err := q.Watch(ctx)
	require.NoError(t, err)

	// Submit events to the empty stream
	sub1 := stream.MustSubmit(ctx, "items", watchTestEvent{ID: "first"})
	sub2 := stream.MustSubmit(ctx, "items", watchTestEvent{ID: "second"})

	// Both events should be deliverable
	id1, err := watch.TickWithID(ctx)
	require.NoError(t, err)
	assert.Equal(t, sub1.ID, id1)

	id2, err := watch.TickWithID(ctx)
	require.NoError(t, err)
	assert.Equal(t, sub2.ID, id2)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []int64{sub1.ID, sub2.ID}, receivedIDs)
}

func TestWatchWithAfterClauseReceivesNewEvents(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	transport := v1.NewMemoryTransport()
	system := v1.NewSystem(transport)
	stream := system.MustStream(ctx, "test-domain", "test-stream")

	// Submit 5 events before watch
	for i := 0; i < 5; i++ {
		stream.MustSubmit(ctx, "items", watchTestEvent{ID: "pre"})
	}

	// Create a watch that only sees events after ID 2
	var mu sync.Mutex
	var receivedIDs []int64

	q := NewQuery(stream)
	q.After(2)
	q.OnKind("items").Each(func(_ context.Context, e v1.Envelope, _ json.RawMessage) error {
		mu.Lock()
		receivedIDs = append(receivedIDs, e.ID)
		mu.Unlock()
		return nil
	})
	watch, err := q.Watch(ctx)
	require.NoError(t, err)

	// Tick through init events (IDs 3 and 4)
	_, err = watch.TickWithID(ctx)
	require.NoError(t, err)
	_, err = watch.TickWithID(ctx)
	require.NoError(t, err)

	mu.Lock()
	initCount := len(receivedIDs)
	mu.Unlock()
	require.Equal(t, 2, initCount, "should deliver 2 init events after After(2)")

	// Submit a new event
	sub := stream.MustSubmit(ctx, "items", watchTestEvent{ID: "post"})

	// The new event should be delivered
	id, err := watch.TickWithID(ctx)
	require.NoError(t, err)
	assert.Equal(t, sub.ID, id)

	mu.Lock()
	defer mu.Unlock()
	assert.Len(t, receivedIDs, 3)
	assert.Equal(t, sub.ID, receivedIDs[2])
}

func TestMemoryWatchInitReplayThenPostInitExactlyOnce(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	transport := v1.NewMemoryTransport()
	system := v1.NewSystem(transport)
	stream := system.MustStream(ctx, "test-domain", "test-stream")

	// Phase 1: pre-submit 5 events before the watch starts
	preCount := 5
	preIDs := make([]int64, 0, preCount)
	for i := range preCount {
		sub := stream.MustSubmit(ctx, "items", watchTestEvent{ID: fmt.Sprintf("pre-%d", i)})
		preIDs = append(preIDs, sub.ID)
	}

	var mu sync.Mutex
	var receivedIDs []int64

	q := NewQuery(stream)
	q.OnKind("items").Each(func(_ context.Context, e v1.Envelope, _ json.RawMessage) error {
		mu.Lock()
		receivedIDs = append(receivedIDs, e.ID)
		mu.Unlock()
		return nil
	})
	watch, err := q.Watch(ctx)
	require.NoError(t, err)

	// Phase 2: consume all init events
	for i := range preCount {
		id, err := watch.TickWithID(ctx)
		require.NoError(t, err, "init tick %d should not error", i)
		assert.Equal(t, preIDs[i], id, "init tick %d should return the expected event ID", i)
	}

	mu.Lock()
	initLen := len(receivedIDs)
	mu.Unlock()
	require.Equal(t, preCount, initLen, "should have received exactly %d init events", preCount)

	// Phase 3: submit 5 more events after init is consumed
	postCount := 5
	postIDs := make([]int64, 0, postCount)
	for i := range postCount {
		sub := stream.MustSubmit(ctx, "items", watchTestEvent{ID: fmt.Sprintf("post-%d", i)})
		postIDs = append(postIDs, sub.ID)
	}

	// Phase 4: consume all post-init events
	for i := range postCount {
		id, err := watch.TickWithID(ctx)
		require.NoError(t, err, "post-init tick %d should not error", i)
		assert.Equal(t, postIDs[i], id, "post-init tick %d should return the expected event ID", i)
	}

	// Phase 5: assert full set — no duplicates, no gaps, correct ordering
	mu.Lock()
	defer mu.Unlock()
	assert.Len(t, receivedIDs, preCount+postCount,
		"should have received exactly %d events total (no duplicates, no extras)", preCount+postCount)

	// Verify no duplicates via set check
	seen := make(map[int64]int)
	for _, id := range receivedIDs {
		seen[id]++
	}
	for id, count := range seen {
		assert.Equal(t, 1, count, "event ID %d was delivered %d times (expected exactly once)", id, count)
	}
}

func TestWatchMultipleRoundsOfNewEvents(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	transport := v1.NewMemoryTransport()
	system := v1.NewSystem(transport)
	stream := system.MustStream(ctx, "test-domain", "test-stream")

	var mu sync.Mutex
	var receivedIDs []int64

	q := NewQuery(stream)
	q.OnKind("items").Each(func(_ context.Context, e v1.Envelope, _ json.RawMessage) error {
		mu.Lock()
		receivedIDs = append(receivedIDs, e.ID)
		mu.Unlock()
		return nil
	})
	watch, err := q.Watch(ctx)
	require.NoError(t, err)

	// Round 1: submit and receive
	sub1 := stream.MustSubmit(ctx, "items", watchTestEvent{ID: "round-1"})
	id1, err := watch.TickWithID(ctx)
	require.NoError(t, err)
	assert.Equal(t, sub1.ID, id1)

	// Round 2: submit and receive
	sub2 := stream.MustSubmit(ctx, "items", watchTestEvent{ID: "round-2"})
	id2, err := watch.TickWithID(ctx)
	require.NoError(t, err)
	assert.Equal(t, sub2.ID, id2)

	// Round 3: submit and receive
	sub3 := stream.MustSubmit(ctx, "items", watchTestEvent{ID: "round-3"})
	id3, err := watch.TickWithID(ctx)
	require.NoError(t, err)
	assert.Equal(t, sub3.ID, id3)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []int64{sub1.ID, sub2.ID, sub3.ID}, receivedIDs,
		"all three rounds should deliver exactly once in order")
}
