package views

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// seedMemoryEntity writes an entity through the store and notifies the notifier,
// mirroring the Indexer's apply-then-notify path.
func seedMemoryEntity(ctx context.Context, t *testing.T, store *MemoryStore, notifier *Notifier, eventID int64, kind string, key Key, value any) {
	t.Helper()
	change, err := store.Persist(ctx, &ReduceResult{
		Upserts: []Upsert{{Kind: kind, Key: key, Value: value}},
	}, eventID)
	require.NoError(t, err)
	require.NoError(t, notifier.Emit(ctx, *change))
}

func deleteMemoryEntity(ctx context.Context, t *testing.T, store *MemoryStore, notifier *Notifier, eventID int64, kind string, key Key) {
	t.Helper()
	change, err := store.Persist(ctx, &ReduceResult{
		Deletes: []Delete{{Kind: kind, Key: key}},
	}, eventID)
	require.NoError(t, err)
	require.NoError(t, notifier.Emit(ctx, *change))
}

func newMemoryReader() (*MemoryStore, *Notifier, *MemoryReader) {
	store := NewMemoryStore()
	notifier := NewNotifier()
	return store, notifier, NewMemoryReader(store, notifier)
}

// getResult carries the outcome of an asynchronous Get across the goroutine
// boundary without calling testing.T inside the goroutine.
type getResult struct {
	entity *Entity
	status Status
	err    error
}

// requireGetBlocks asserts an in-flight Get has not returned yet, proving the
// caller actually blocked on the version wait instead of short-circuiting. The
// target version is never reached during the wait window, so any receive means
// the wait path regressed.
func requireGetBlocks(t *testing.T, done <-chan getResult) {
	t.Helper()
	select {
	case res := <-done:
		require.Failf(t, "Get returned before target was reached", "status=%v err=%v", res.status, res.err)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestMemoryReaderGetVersionConstrained(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		seed        bool
		seedVersion int64
		opts        []GetOption
		wantStatus  Status
		wantVersion int64
	}{
		{"plain", true, 1, nil, StatusOK, 1},
		{"plain not found", false, 0, nil, StatusNotFound, 0},
		{"after satisfied", true, 3, []GetOption{After(2)}, StatusOK, 3},
		{"after equal to current version", true, 3, []GetOption{After(3)}, StatusOK, 3},
		{"after stale", true, 1, []GetOption{After(5)}, StatusStale, 1},
		{"after not found", false, 0, []GetOption{After(1)}, StatusNotFound, 0},
		{"until already reached", true, 3, []GetOption{UntilVersion(2, time.Second)}, StatusOK, 3},
		{"until equal to current version", true, 3, []GetOption{UntilVersion(3, time.Second)}, StatusOK, 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store, notifier, reader := newMemoryReader()
			if tc.seed {
				seedMemoryEntity(t.Context(), t, store, notifier, tc.seedVersion, "items", NewKey("a"), map[string]any{"name": "thing"})
			}

			entity, status, err := reader.Get(t.Context(), "items", NewKey("a"), tc.opts...)
			require.NoError(t, err)
			require.Equal(t, tc.wantStatus, status)
			if tc.wantStatus == StatusNotFound {
				require.Nil(t, entity)
				return
			}
			require.NotNil(t, entity)
			require.Equal(t, tc.wantVersion, entity.Version)
		})
	}
}

// TestMemoryReaderGetReturnsStoredEntity pins the stored entity round-trip: the
// payload survives JSON marshaling and the identity fields match the seed.
func TestMemoryReaderGetReturnsStoredEntity(t *testing.T) {
	t.Parallel()
	store, notifier, reader := newMemoryReader()
	seedMemoryEntity(t.Context(), t, store, notifier, 1, "items", NewKey("a"), map[string]any{"name": "thing"})

	entity, status, err := reader.Get(t.Context(), "items", NewKey("a"))
	require.NoError(t, err)
	require.Equal(t, StatusOK, status)
	require.NotNil(t, entity)
	require.Equal(t, "items", entity.Kind)
	require.Equal(t, NewKey("a"), entity.Key)
	require.Equal(t, int64(1), entity.Version)
	require.JSONEq(t, `{"name":"thing"}`, string(entity.Value))
}

func TestMemoryReaderGetUntilVersionWaits(t *testing.T) {
	t.Parallel()
	store, notifier, reader := newMemoryReader()
	seedMemoryEntity(t.Context(), t, store, notifier, 1, "items", NewKey("a"), map[string]any{"name": "first"})

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	t.Cleanup(cancel)

	started := make(chan struct{})
	done := make(chan getResult, 1)
	go func() {
		close(started)
		entity, status, err := reader.Get(ctx, "items", NewKey("a"), UntilVersion(3, 500*time.Millisecond))
		done <- getResult{entity: entity, status: status, err: err}
	}()
	<-started

	requireGetBlocks(t, done)

	seedMemoryEntity(t.Context(), t, store, notifier, 3, "items", NewKey("a"), map[string]any{"name": "second"})

	select {
	case res := <-done:
		require.NoError(t, res.err)
		require.Equal(t, StatusOK, res.status)
		require.NotNil(t, res.entity)
		require.Equal(t, int64(3), res.entity.Version)
		require.JSONEq(t, `{"name":"second"}`, string(res.entity.Value))
	case <-time.After(time.Second):
		require.Fail(t, "Get did not return after target version was reached")
	}
}

func TestMemoryReaderGetUntilVersionReachedButAbsent(t *testing.T) {
	t.Parallel()
	store, notifier, reader := newMemoryReader()
	seedMemoryEntity(t.Context(), t, store, notifier, 1, "items", NewKey("a"), map[string]any{"name": "thing"})

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	started := make(chan struct{})
	done := make(chan getResult, 1)
	go func() {
		close(started)
		entity, status, err := reader.Get(ctx, "items", NewKey("a"), UntilVersion(3, 500*time.Millisecond))
		done <- getResult{entity: entity, status: status, err: err}
	}()
	<-started

	requireGetBlocks(t, done)

	deleteMemoryEntity(t.Context(), t, store, notifier, 3, "items", NewKey("a"))

	select {
	case res := <-done:
		require.NoError(t, res.err)
		require.Nil(t, res.entity)
		require.Equal(t, StatusNotFound, res.status)
	case <-time.After(time.Second):
		require.Fail(t, "Get did not return after target version was reached")
	}
}

func TestMemoryReaderGetUntilVersionTimeout(t *testing.T) {
	t.Parallel()
	store, notifier, reader := newMemoryReader()
	seedMemoryEntity(t.Context(), t, store, notifier, 1, "items", NewKey("a"), map[string]any{"name": "thing"})

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	entity, status, err := reader.Get(ctx, "items", NewKey("a"), UntilVersion(10, 50*time.Millisecond))
	require.NoError(t, err)
	require.Equal(t, StatusTimeout, status)
	require.NotNil(t, entity)
	require.Equal(t, int64(1), entity.Version)
}

func TestMemoryReaderGetUntilVersionContextCanceled(t *testing.T) {
	t.Parallel()
	store, notifier, reader := newMemoryReader()
	seedMemoryEntity(t.Context(), t, store, notifier, 1, "items", NewKey("a"), map[string]any{"name": "thing"})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	entity, _, err := reader.Get(ctx, "items", NewKey("a"), UntilVersion(5, time.Second))
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, entity)
}

func TestMemoryReaderVersion(t *testing.T) {
	t.Parallel()
	store, notifier, reader := newMemoryReader()
	seedMemoryEntity(t.Context(), t, store, notifier, 7, "items", NewKey("a"), map[string]any{"name": "thing"})

	version, err := reader.Version(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(7), version)
}
