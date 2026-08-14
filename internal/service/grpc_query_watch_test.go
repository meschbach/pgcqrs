//go:build testcontainers_pg

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-faker/faker/v4"
	"github.com/jackc/pgx/v5/pgxpool"
	storage2 "github.com/meschbach/pgcqrs/internal/service/storage"
	"github.com/meschbach/pgcqrs/pkg/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

type threadSafeCapturingQueryServer struct {
	ctx  context.Context
	mu   sync.Mutex
	sent []*ipc.QueryOut
}

func (c *threadSafeCapturingQueryServer) Send(msg *ipc.QueryOut) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, msg)
	return nil
}

func (c *threadSafeCapturingQueryServer) Snapshot() []*ipc.QueryOut {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*ipc.QueryOut, len(c.sent))
	copy(out, c.sent)
	return out
}

func (c *threadSafeCapturingQueryServer) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sent)
}

func (c *threadSafeCapturingQueryServer) Context() context.Context     { return c.ctx }
func (c *threadSafeCapturingQueryServer) SetHeader(metadata.MD) error  { return nil }
func (c *threadSafeCapturingQueryServer) SendHeader(metadata.MD) error { return nil }
func (c *threadSafeCapturingQueryServer) SetTrailer(metadata.MD)       {}
func (c *threadSafeCapturingQueryServer) SendMsg(interface{}) error    { return nil }
func (c *threadSafeCapturingQueryServer) RecvMsg(interface{}) error    { return nil }

func TestGrpcQueryWatch_SendsExistingEvents(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	app := faker.Word()
	stream := faker.Word()
	kind := "watch_existing_" + faker.Word()

	id1 := insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"n": "1"})
	id2 := insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"n": "2"})

	q := &grpcQuery{
		core: storage2.RepositoryWithPool(pool),
		bus:  newBus(),
	}

	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	mock := &threadSafeCapturingQueryServer{ctx: watchCtx}

	queryIn := &ipc.QueryIn{
		Events: &ipc.DomainStream{Domain: app, Stream: stream},
		OnKind: []*ipc.OnKindClause{
			{Kind: kind, AllOp: ptrInt64(0)},
		},
	}

	done := make(chan error, 1)
	go func() { done <- q.Watch(queryIn, mock) }()

	require.Eventually(t, func() bool {
		return mock.Count() >= 2
	}, 5*time.Second, 10*time.Millisecond, "Watch should send both events")

	cancel()
	<-done

	sent := mock.Snapshot()
	require.Len(t, sent, 2)
	assert.Equal(t, id1, *sent[0].Id)
	assert.Equal(t, id2, *sent[1].Id)
}

func TestGrpcQueryWatch_ReQueryOnNewEvent(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	app := faker.Word()
	stream := faker.Word()
	kind := "watch_requery_" + faker.Word()

	id1 := insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"n": "1"})

	q := &grpcQuery{
		core: storage2.RepositoryWithPool(pool),
		bus:  newBus(),
	}

	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	mock := &threadSafeCapturingQueryServer{ctx: watchCtx}

	queryIn := &ipc.QueryIn{
		Events: &ipc.DomainStream{Domain: app, Stream: stream},
		OnKind: []*ipc.OnKindClause{
			{Kind: kind, AllOp: ptrInt64(0)},
		},
	}

	done := make(chan error, 1)
	go func() { done <- q.Watch(queryIn, mock) }()

	require.Eventually(t, func() bool {
		return mock.Count() >= 1
	}, 5*time.Second, 10*time.Millisecond, "Watch should send first event")

	id2 := insertEventAndDispatch(t, ctx, pool, q.bus, app, stream, kind, map[string]string{"n": "2"})

	require.Eventually(t, func() bool {
		return mock.Count() >= 2
	}, 5*time.Second, 10*time.Millisecond, "Watch should send second event after re-query")

	cancel()
	<-done

	sent := mock.Snapshot()
	require.Len(t, sent, 2)
	assert.Equal(t, id1, *sent[0].Id)
	assert.Equal(t, id2, *sent[1].Id)
}

func TestGrpcQueryWatch_DeduplicatesAcrossReQuery(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	app := faker.Word()
	stream := faker.Word()
	kind := "watch_dedup_" + faker.Word()

	id1 := insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"n": "1"})

	q := &grpcQuery{
		core: storage2.RepositoryWithPool(pool),
		bus:  newBus(),
	}

	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	mock := &threadSafeCapturingQueryServer{ctx: watchCtx}

	queryIn := &ipc.QueryIn{
		Events: &ipc.DomainStream{Domain: app, Stream: stream},
		OnKind: []*ipc.OnKindClause{
			{Kind: kind, AllOp: ptrInt64(0)},
		},
	}

	done := make(chan error, 1)
	go func() { done <- q.Watch(queryIn, mock) }()

	require.Eventually(t, func() bool {
		return mock.Count() >= 1
	}, 5*time.Second, 10*time.Millisecond, "Watch should send first event")

	id2 := insertEventAndDispatch(t, ctx, pool, q.bus, app, stream, kind, map[string]string{"n": "2"})

	require.Eventually(t, func() bool {
		return mock.Count() >= 2
	}, 5*time.Second, 10*time.Millisecond, "Watch should send second event")

	cancel()
	<-done

	sent := mock.Snapshot()
	var id1Count int
	for _, msg := range sent {
		if *msg.Id == id1 {
			id1Count++
		}
	}
	assert.Equal(t, 1, id1Count, "event 1 should be sent exactly once")
	assert.Equal(t, id2, *sent[1].Id, "event 2 should follow event 1")
}

func TestGrpcQueryWatch_ContextCancellationStopsLoop(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	app := faker.Word()
	stream := faker.Word()
	kind := "watch_cancel_" + faker.Word()

	insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"n": "1"})

	q := &grpcQuery{
		core: storage2.RepositoryWithPool(pool),
		bus:  newBus(),
	}

	watchCtx, cancel := context.WithCancel(ctx)
	mock := &threadSafeCapturingQueryServer{ctx: watchCtx}

	queryIn := &ipc.QueryIn{
		Events: &ipc.DomainStream{Domain: app, Stream: stream},
		OnKind: []*ipc.OnKindClause{
			{Kind: kind, AllOp: ptrInt64(0)},
		},
	}

	done := make(chan error, 1)
	go func() { done <- q.Watch(queryIn, mock) }()

	require.Eventually(t, func() bool {
		return mock.Count() >= 1
	}, 5*time.Second, 10*time.Millisecond, "Watch should send event")

	cancel()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("Watch handler did not exit after context cancellation")
	}
}

func TestGrpcQueryWatch_DedupUnderRapidInsert(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	app := faker.Word()
	stream := faker.Word()
	kind := "watch_rapid_" + faker.Word()

	id1 := insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"n": "1"})

	q := &grpcQuery{
		core: storage2.RepositoryWithPool(pool),
		bus:  newBus(),
	}

	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	mock := &threadSafeCapturingQueryServer{ctx: watchCtx}

	queryIn := &ipc.QueryIn{
		Events: &ipc.DomainStream{Domain: app, Stream: stream},
		OnKind: []*ipc.OnKindClause{
			{Kind: kind, AllOp: ptrInt64(0)},
		},
	}

	done := make(chan error, 1)
	go func() { done <- q.Watch(queryIn, mock) }()

	// Wait for initial event to be delivered
	require.Eventually(t, func() bool {
		return mock.Count() >= 1
	}, 5*time.Second, 10*time.Millisecond, "Watch should send first event")

	// Fire multiple events rapidly — some may land during processWatchResults
	var ids []int64
	for i := 0; i < 5; i++ {
		n := map[string]string{"n": fmt.Sprintf("rapid-%d", i)}
		id := insertEventAndDispatch(t, ctx, pool, q.bus, app, stream, kind, n)
		ids = append(ids, id)
	}

	// Wait for all 6 events (1 initial + 5 rapid)
	require.Eventually(t, func() bool {
		return mock.Count() >= 6
	}, 5*time.Second, 10*time.Millisecond, "Watch should deliver all 6 events")

	cancel()
	<-done

	// Verify exactly-once delivery
	sent := mock.Snapshot()
	require.Len(t, sent, 6)

	// Verify the initial event is among those delivered
	assert.Contains(t, sentIDs(sent), id1, "initial event should be delivered")

	seen := make(map[int64]int)
	for _, msg := range sent {
		seen[*msg.Id]++
	}
	for id, count := range seen {
		assert.Equal(t, 1, count, "event %d should be sent exactly once, was sent %d times", id, count)
	}
}

func TestGrpcQueryWatch_InitReplayThenPostInitExactlyOnce(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	app := faker.Word()
	stream := faker.Word()
	kind := "watch_init_post_" + faker.Word()

	// Phase 1: pre-insert 5 events (no bus dispatch — just DB)
	preCount := 5
	var preIDs []int64
	for i := range preCount {
		id := insertEvent(ctx, t, pool, app, stream, kind, map[string]string{"n": fmt.Sprintf("pre-%d", i)})
		preIDs = append(preIDs, id)
	}

	q := &grpcQuery{
		core: storage2.RepositoryWithPool(pool),
		bus:  newBus(),
	}

	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	mock := &threadSafeCapturingQueryServer{ctx: watchCtx}

	queryIn := &ipc.QueryIn{
		Events: &ipc.DomainStream{Domain: app, Stream: stream},
		OnKind: []*ipc.OnKindClause{
			{Kind: kind, AllOp: ptrInt64(0)},
		},
	}

	done := make(chan error, 1)
	go func() { done <- q.Watch(queryIn, mock) }()

	// Phase 2: wait for all init events
	require.Eventually(t, func() bool {
		return mock.Count() >= preCount
	}, 5*time.Second, 10*time.Millisecond, "Watch should deliver all %d init events", preCount)

	cancel()
	<-done

	sent := mock.Snapshot()
	require.Len(t, sent, preCount, "should have received exactly %d init events", preCount)

	// Verify init events arrived in order
	for i := range preCount {
		assert.Equal(t, preIDs[i], *sent[i].Id, "init event %d should match pre-inserted ID", i)
	}

	// Phase 3: start a fresh watch and inject post-init events
	watchCtx2, cancel2 := context.WithCancel(ctx)
	defer cancel2()
	mock2 := &threadSafeCapturingQueryServer{ctx: watchCtx2}

	done2 := make(chan error, 1)
	go func() { done2 <- q.Watch(queryIn, mock2) }()

	// Wait for init events to be delivered on the new watch
	require.Eventually(t, func() bool {
		return mock2.Count() >= preCount
	}, 5*time.Second, 10*time.Millisecond, "second Watch should deliver init events")

	// Phase 4: insert 5 more events + dispatch
	postCount := 5
	var postIDs []int64
	for i := range postCount {
		id := insertEventAndDispatch(t, ctx, pool, q.bus, app, stream, kind, map[string]string{"n": fmt.Sprintf("post-%d", i)})
		postIDs = append(postIDs, id)
	}

	// Wait for all init + post events
	require.Eventually(t, func() bool {
		return mock2.Count() >= preCount+postCount
	}, 5*time.Second, 10*time.Millisecond, "Watch should deliver all %d events", preCount+postCount)

	cancel2()
	<-done2

	sent2 := mock2.Snapshot()
	require.Len(t, sent2, preCount+postCount,
		"should have received exactly %d events (no duplicates, no extras)", preCount+postCount)

	// Verify full ordering: init events first, then post-init events
	for i := range preCount {
		assert.Equal(t, preIDs[i], *sent2[i].Id, "init event %d ordering", i)
	}
	for i := range postCount {
		assert.Equal(t, postIDs[i], *sent2[preCount+i].Id, "post-init event %d ordering", i)
	}

	// Verify no duplicates
	seen := make(map[int64]int)
	for _, msg := range sent2 {
		seen[*msg.Id]++
	}
	for id, count := range seen {
		assert.Equal(t, 1, count, "event %d was sent %d times (expected exactly once)", id, count)
	}
}

func ptrInt64(v int64) *int64 {
	return &v
}

func sentIDs(sent []*ipc.QueryOut) []int64 {
	ids := make([]int64, len(sent))
	for i, msg := range sent {
		ids[i] = *msg.Id
	}
	return ids
}

func insertEventAndDispatch(t *testing.T, ctx context.Context, pool *pgxpool.Pool, bus *bus, app, stream, kind string, event map[string]string) int64 {
	t.Helper()
	id := insertEvent(ctx, t, pool, app, stream, kind, event)
	body, err := json.Marshal(event)
	require.NoError(t, err)
	bus.dispatchOnEventStored(ctx, app, stream, id, kind, body)
	return id
}
