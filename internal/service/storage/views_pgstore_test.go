//go:build testcontainers_pg

package storage

import (
	"fmt"
	"testing"

	"github.com/meschbach/pgcqrs/pkg/indexer/views"
	"github.com/stretchr/testify/require"
)

func TestViewsPGStorePersistAdvancesPosition(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	ctx := t.Context()

	domain := fmt.Sprintf("domain-%s", t.Name())
	stream := fmt.Sprintf("stream-%s", t.Name())
	createTestStream(ctx, t, pool, domain, stream)

	id := views.NewProjectionIdentity(domain, stream, "items")
	store := views.NewPGStore(pool)

	change, err := store.Persist(ctx, id, &views.ReduceResult{
		Upserts: []views.Upsert{
			{Kind: "items", Key: views.NewKey("item-42"), Value: map[string]any{"name": "first"}},
		},
	}, 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), change.Version)

	version, err := store.Version(ctx, id)
	require.NoError(t, err)
	require.Equal(t, int64(1), version)

	entity, err := store.Get(ctx, id, "items", views.NewKey("item-42"))
	require.NoError(t, err)
	require.NotNil(t, entity)
	require.Equal(t, int64(1), entity.Version)

	// A nil result (no mutations) must still advance the projection version
	// by writing a consumer_positions row in the same transaction.
	change, err = store.Persist(ctx, id, nil, 2)
	require.NoError(t, err)
	require.Equal(t, int64(2), change.Version)

	version, err = store.Version(ctx, id)
	require.NoError(t, err)
	require.Equal(t, int64(2), version)

	// The previously written entity is untouched by the empty apply.
	entity, err = store.Get(ctx, id, "items", views.NewKey("item-42"))
	require.NoError(t, err)
	require.NotNil(t, entity)
	require.Equal(t, int64(1), entity.Version)

	// A stale (lower) event ID must not regress the position.
	_, err = store.Persist(ctx, id, nil, 1)
	require.NoError(t, err)

	version, err = store.Version(ctx, id)
	require.NoError(t, err)
	require.Equal(t, int64(2), version)
}
