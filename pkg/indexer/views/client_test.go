package views

import (
	"context"
	"fmt"
	"testing"
	"time"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Package views_test contains unit tests for the views package Client API.
//
// Testing Strategy:
// - Unit tests (this package): Test Client API behavior with real event flow
//   - Use MemoryTransport for speed and isolation
//   - Submit events through the stream (not direct store manipulation)
//   - Use UntilVersion to wait for event processing
//   - Test user-facing behavior, not internal implementation details
//
// - Integration tests (systest/views_test.go): Test full gRPC flow
//   - Test with real transport (memory or gRPC based on PGCQRS_TEST_TRANSPORT)
//   - Test end-to-end user experience
//   - Test across multiple projections and streams
//
// What NOT to test in unit tests:
// - Internal store types (MemoryStore, PGStore)
// - Direct store manipulation (bypassing event flow)
// - Implementation details that users don't interact with

type testEvent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// setupClientWithEvents creates a client and submits numEvents events,
// waiting for them to be processed before returning.
func setupClientWithEvents(t *testing.T, numEvents int) (context.Context, ProjectionClient, *v1.Stream) {
	t.Helper()
	transport := v1.NewMemoryTransport()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)

	sys := v1.NewSystem(transport)
	stream, err := sys.Stream(ctx, "domain", "stream")
	require.NoError(t, err)

	proj := NewProjection("domain", "stream",
		OnKind("TestEvent", func(_ context.Context, _ v1.Envelope, evt *testEvent, _ *ReduceContext) (*ReduceResult, error) {
			return &ReduceResult{
				Upserts: []Upsert{
					{Kind: "items", Key: NewKey("item-1"), Value: evt},
				},
			}, nil
		}),
	)

	client, err := With(ctx, sys, proj)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, client.Close())
	})

	// Submit events
	var lastID int64
	for i := 0; i < numEvents; i++ {
		sub, err := stream.Submit(ctx, "TestEvent", testEvent{
			ID:   fmt.Sprintf("event-%d", i),
			Name: "Widget",
		})
		require.NoError(t, err)
		lastID = sub.ID
	}

	// Wait for processing
	_, result, err := client.Get(ctx, "items", NewKey("item-1"), UntilVersion(lastID, 5*time.Second))
	require.NoError(t, err)
	require.Equal(t, StatusOK, result.Status)

	return ctx, client, stream
}

func TestClientGetNotFound(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	sys := v1.NewSystem(transport)
	proj := NewProjection("domain", "stream")
	client, err := With(ctx, sys, proj)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, client.Close())
	}()

	entity, result, err := client.Get(ctx, "items", NewKey("nonexistent"))
	require.NoError(t, err)
	assert.Nil(t, entity)
	assert.Equal(t, StatusNotFound, result.Status)
}

func TestClientGetAfterSatisfied(t *testing.T) {
	t.Parallel()
	ctx, client, _ := setupClientWithEvents(t, 5)

	// After submitting 5 events (IDs 0-4), version is 4
	entity, result, err := client.Get(ctx, "items", NewKey("item-1"), After(3))
	require.NoError(t, err)
	assert.Equal(t, StatusOK, result.Status)
	require.NotNil(t, entity)
	assert.Equal(t, int64(4), entity.Version)
}

func TestClientGetAfterStale(t *testing.T) {
	t.Parallel()
	ctx, client, _ := setupClientWithEvents(t, 2)

	// After submitting 2 events (IDs 0-1), version is 1
	entity, result, err := client.Get(ctx, "items", NewKey("item-1"), After(5))
	require.NoError(t, err)
	assert.Equal(t, StatusStale, result.Status)
	require.NotNil(t, entity)
	assert.Equal(t, int64(1), entity.Version)
}

func TestClientGetUntilVersionWaitsForNilEntity(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	sys := v1.NewSystem(transport)
	proj := NewProjection("domain", "stream")
	client, err := With(ctx, sys, proj)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, client.Close())
	}()

	// Call Get with UntilVersion on a non-existent entity using a short-lived context.
	// If Get returns immediately (bug), it returns StatusNotFound.
	// If Get blocks correctly (fix), it waits until the context expires.
	shortCtx, shortCancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer shortCancel()

	_, result, err := client.Get(shortCtx, "items", NewKey("item-1"), UntilVersion(5, 10*time.Second))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Nil(t, result)
}

func TestClientGetUntilVersionTimeout(t *testing.T) {
	t.Parallel()
	ctx, client, _ := setupClientWithEvents(t, 2)

	// After submitting 2 events (IDs 0-1), version is 1
	entity, result, err := client.Get(ctx, "items", NewKey("item-1"), UntilVersion(10, 50*time.Millisecond))
	require.NoError(t, err)
	assert.Equal(t, StatusTimeout, result.Status)
	require.NotNil(t, entity)
	assert.Equal(t, int64(1), entity.Version)
}

func TestClientVersion(t *testing.T) {
	t.Parallel()
	ctx, client, _ := setupClientWithEvents(t, 42)

	// After submitting 42 events (IDs 0-41), version is 41
	version, err := client.Version(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(41), version)
}

func TestClientOnChange(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	sys := v1.NewSystem(transport)
	proj := NewProjection("domain", "stream")
	client, err := With(ctx, sys, proj)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, client.Close())
	}()
	memClient, ok := client.(*Client[*v1.MemoryLock])
	require.True(t, ok)

	var changes []Change
	client.OnChange(func(_ context.Context, c Change) error {
		changes = append(changes, c)
		return nil
	})

	// Trigger a notification
	require.NoError(t, memClient.notifier.Emit(ctx, Change{Version: 1, Upserts: []Upsert{{Kind: "items", Key: NewKey("item-1"), Value: "data"}}}))

	require.Len(t, changes, 1)
	assert.Equal(t, int64(1), changes[0].Version)
}

func TestClientClose(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	sys := v1.NewSystem(transport)
	proj := NewProjection("domain", "stream")
	client, err := With(ctx, sys, proj)
	require.NoError(t, err)

	err = client.Close()
	require.NoError(t, err)
}

func TestClientOnChangeUnsubscribe(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	sys := v1.NewSystem(transport)
	proj := NewProjection("domain", "stream")
	client, err := With(ctx, sys, proj)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, client.Close())
	}()
	memClient, ok := client.(*Client[*v1.MemoryLock])
	require.True(t, ok)

	var changes []Change
	unsub := client.OnChange(func(_ context.Context, c Change) error {
		changes = append(changes, c)
		return nil
	})

	require.NoError(t, memClient.notifier.Emit(ctx, Change{Version: 1}))
	require.Len(t, changes, 1)

	unsub()

	require.NoError(t, memClient.notifier.Emit(ctx, Change{Version: 2}))
	assert.Len(t, changes, 1, "callback should not receive after unsubscribe")
}

func TestClientOnChangeUnsubscribeDoesNotAffectOthers(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	sys := v1.NewSystem(transport)
	proj := NewProjection("domain", "stream")
	client, err := With(ctx, sys, proj)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, client.Close())
	}()
	memClient, ok := client.(*Client[*v1.MemoryLock])
	require.True(t, ok)

	var changes1, changes2 []Change
	unsub1 := client.OnChange(func(_ context.Context, c Change) error {
		changes1 = append(changes1, c)
		return nil
	})
	client.OnChange(func(_ context.Context, c Change) error {
		changes2 = append(changes2, c)
		return nil
	})

	require.NoError(t, memClient.notifier.Emit(ctx, Change{Version: 1}))
	require.Len(t, changes1, 1)
	require.Len(t, changes2, 1)

	unsub1()

	require.NoError(t, memClient.notifier.Emit(ctx, Change{Version: 2}))
	assert.Len(t, changes1, 1, "unsubscribed callback should not receive")
	assert.Len(t, changes2, 2, "other callback should still receive")
}

func TestClientGetUntilVersionStoreAlreadyAtTarget(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	sys := v1.NewSystem(transport)
	stream, err := sys.Stream(ctx, "domain", "stream")
	require.NoError(t, err)

	type FooData struct {
		Name string `json:"name"`
	}
	type BarData struct {
		Value int `json:"value"`
	}

	proj := NewProjection("domain", "stream",
		OnKind("FooEvent", func(_ context.Context, _ v1.Envelope, evt *FooData, _ *ReduceContext) (*ReduceResult, error) {
			return &ReduceResult{
				Upserts: []Upsert{
					{Kind: "foos", Key: NewKey("the-foo"), Value: evt},
				},
			}, nil
		}),
		OnKind("BarEvent", func(_ context.Context, _ v1.Envelope, evt *BarData, _ *ReduceContext) (*ReduceResult, error) {
			return &ReduceResult{
				Upserts: []Upsert{
					{Kind: "bars", Key: NewKey("the-bar"), Value: evt},
				},
			}, nil
		}),
	)

	client, err := With(ctx, sys, proj)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	// Events: ID 0, 1 → the-foo    ID 2 → the-bar (independent entity kind)
	_, err = stream.Submit(ctx, "FooEvent", FooData{Name: "first"})
	require.NoError(t, err)
	_, err = stream.Submit(ctx, "FooEvent", FooData{Name: "second"})
	require.NoError(t, err)
	barSubmit, err := stream.Submit(ctx, "BarEvent", BarData{Value: 42})
	require.NoError(t, err)

	// Synchronize: ensure pump has processed event 2 before we test
	_, syncResult, err := client.Get(ctx, "bars", NewKey("the-bar"), UntilVersion(barSubmit.ID, 5*time.Second))
	require.NoError(t, err)
	require.Equal(t, StatusOK, syncResult.Status)

	// Get the-foo at target version 2. Entity the-foo is at version 1 (never
	// touched by event 2), so the Get goes through waitForVersion. The store
	// is already at version 2, so storeAlreadyAtVersion should refetch and
	// return the entity. Without the fix this would time out (50ms).
	entity, result, err := client.Get(ctx, "foos", NewKey("the-foo"), UntilVersion(barSubmit.ID, 50*time.Millisecond))
	require.NoError(t, err)
	require.Equal(t, StatusOK, result.Status, "without storeAlreadyAtVersion fix this would timeout")
	require.NotNil(t, entity)
	assert.Equal(t, int64(1), entity.Version, "the-foo was last updated by event 1")
}

func TestWithMemorySystem(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	sys := v1.NewSystem(transport)
	proj := NewProjection("domain", "stream")
	client, err := With(ctx, sys, proj)
	require.NoError(t, err)
	require.NotNil(t, client)
	defer func() {
		require.NoError(t, client.Close())
	}()

	concrete, ok := client.(*Client[*v1.MemoryLock])
	require.True(t, ok)
	assert.NotNil(t, concrete.store)
	assert.NotNil(t, concrete.notifier)
	assert.NotNil(t, concrete.pump)
}
