package views

import (
	"context"
	"encoding/json"
	"testing"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewKeySingle(t *testing.T) {
	t.Parallel()
	k := NewKey("item-42")
	assert.Equal(t, []string{"item-42"}, k.Parts())
	assert.True(t, k.IsSingle())
	assert.False(t, k.IsComposite())
}

func TestNewKeyComposite(t *testing.T) {
	t.Parallel()
	k := NewKey("item-42", "location-A")
	assert.Equal(t, []string{"item-42", "location-A"}, k.Parts())
	assert.False(t, k.IsSingle())
	assert.True(t, k.IsComposite())
}

func TestNewKeyPanicsOnOverflow(t *testing.T) {
	t.Parallel()
	require.Panics(t, func() {
		NewKey("a", "b", "c")
	})
}

func TestMemoryStoreGetPut(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore()
	ctx := t.Context()

	// Get non-existent
	entity, err := store.Get(ctx, "items", NewKey("item-42"))
	require.NoError(t, err)
	assert.Nil(t, entity)

	// Persist an upsert
	result := &ReduceResult{
		Upserts: []Upsert{
			{Kind: "items", Key: NewKey("item-42"), Value: map[string]string{"name": "Widget"}},
		},
	}
	change, err := store.Persist(ctx, result, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(100), change.Version)
	assert.Len(t, change.Upserts, 1)

	// Get it back
	entity, err = store.Get(ctx, "items", NewKey("item-42"))
	require.NoError(t, err)
	require.NotNil(t, entity)
	assert.Equal(t, "items", entity.Kind)
	assert.Equal(t, int64(100), entity.Version)

	var data map[string]string
	err = json.Unmarshal(entity.Value, &data)
	require.NoError(t, err)
	assert.Equal(t, "Widget", data["name"])
}

func TestMemoryStoreDelete(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore()
	ctx := t.Context()

	// Upsert then delete
	result := &ReduceResult{
		Upserts: []Upsert{
			{Kind: "items", Key: NewKey("item-42"), Value: "data"},
		},
	}
	_, err := store.Persist(ctx, result, 1)
	require.NoError(t, err)

	result = &ReduceResult{
		Deletes: []Delete{
			{Kind: "items", Key: NewKey("item-42")},
		},
	}
	_, err = store.Persist(ctx, result, 2)
	require.NoError(t, err)

	// Should be gone
	entity, err := store.Get(ctx, "items", NewKey("item-42"))
	require.NoError(t, err)
	assert.Nil(t, entity)
}

func TestMemoryStoreCompositeKey(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore()
	ctx := t.Context()

	result := &ReduceResult{
		Upserts: []Upsert{
			{Kind: "locations", Key: NewKey("item-42", "loc-A"), Value: "data"},
		},
	}
	_, err := store.Persist(ctx, result, 1)
	require.NoError(t, err)

	entity, err := store.Get(ctx, "locations", NewKey("item-42", "loc-A"))
	require.NoError(t, err)
	require.NotNil(t, entity)

	// Different composite key should not match
	entity, err = store.Get(ctx, "locations", NewKey("item-42", "loc-B"))
	require.NoError(t, err)
	assert.Nil(t, entity)
}

func TestMemoryStoreVersionTracking(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore()
	assert.Equal(t, int64(0), store.Version())

	ctx := t.Context()
	result := &ReduceResult{
		Upserts: []Upsert{
			{Kind: "items", Key: NewKey("a"), Value: "x"},
		},
	}
	_, err := store.Persist(ctx, result, 42)
	require.NoError(t, err)
	assert.Equal(t, int64(42), store.Version())
}

func TestReduceContextGet(t *testing.T) {
	t.Parallel()
	expected := &Entity{Kind: "items", Key: NewKey("item-42"), Version: 10}
	rc := &ReduceContext{
		get: func(kind string, key Key) (*Entity, error) {
			assert.Equal(t, "items", kind)
			assert.Equal(t, NewKey("item-42"), key)
			return expected, nil
		},
	}

	entity, err := rc.Get("items", NewKey("item-42"))
	require.NoError(t, err)
	assert.Equal(t, expected, entity)
}

func TestNotifierCallbacks(t *testing.T) {
	t.Parallel()
	n := NewNotifier()
	var received []Change
	n.OnChange(func(c Change) {
		received = append(received, c)
	})

	n.Notify(Change{Version: 1})
	n.Notify(Change{Version: 2})

	require.Len(t, received, 2)
	assert.Equal(t, int64(1), received[0].Version)
	assert.Equal(t, int64(2), received[1].Version)
}

func TestStatusConstants(t *testing.T) {
	t.Parallel()
	assert.Equal(t, StatusOK, Status(0))
	assert.Equal(t, StatusNotFound, Status(1))
	assert.Equal(t, StatusStale, Status(2))
	assert.Equal(t, StatusTimeout, Status(3))
}

func TestProjectionOnKind(t *testing.T) {
	t.Parallel()
	type ItemCreated struct {
		ItemID string `json:"itemID"`
	}

	proj := NewProjection("inventory", "items",
		OnKind("ItemCreated", func(_ context.Context, _ v1.Envelope, _ *ItemCreated, _ *ReduceContext) (*ReduceResult, error) {
			return &ReduceResult{}, nil
		}),
	)

	assert.Equal(t, "inventory", proj.consumerName)
	assert.Len(t, proj.handlers, 1)
	_, ok := proj.handlers["ItemCreated"]
	assert.True(t, ok)
}

func TestProjectionConsumerNameOverride(t *testing.T) {
	t.Parallel()
	proj := NewProjection("inventory", "items", ConsumerName("inventory-v2"))
	assert.Equal(t, "inventory-v2", proj.consumerName)
}
