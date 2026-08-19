package views

import (
	"context"
	"encoding/json"
	"testing"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testItemCreated struct {
	ItemID string `json:"itemID"`
	Name   string `json:"name"`
}

func TestIndexerQueryRegistersHandlers(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	ctx := t.Context()
	stream, err := sys.Stream(ctx, "domain", "stream")
	require.NoError(t, err)

	proj := NewProjection("domain", "stream",
		OnKind("ItemCreated", func(_ context.Context, _ v1.Envelope, _ *testItemCreated, _ *ReduceContext) (*ReduceResult, error) {
			return &ReduceResult{}, nil
		}),
		OnKind("ItemDeleted", func(_ context.Context, _ v1.Envelope, _ *testItemCreated, _ *ReduceContext) (*ReduceResult, error) {
			return &ReduceResult{}, nil
		}),
	)

	store := NewMemoryStore()
	notifier := NewNotifier()
	indexer := NewIndexer(proj, store, notifier, stream)

	q := indexer.Query()
	assert.NotNil(t, q)
}

func TestIndexerHandlerAppliesMutations(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	ctx := t.Context()
	stream, err := sys.Stream(ctx, "domain", "stream")
	require.NoError(t, err)

	proj := NewProjection("domain", "stream",
		OnKind("ItemCreated", func(_ context.Context, _ v1.Envelope, evt *testItemCreated, _ *ReduceContext) (*ReduceResult, error) {
			return &ReduceResult{
				Upserts: []Upsert{
					{Kind: "items", Key: NewKey(evt.ItemID), Value: evt},
				},
			}, nil
		}),
	)

	store := NewMemoryStore()
	notifier := NewNotifier()
	indexer := NewIndexer(proj, store, notifier, stream)

	// Build the dispatch handler and invoke it directly
	handler := indexer.makeDispatchHandler("ItemCreated", proj.handlers["ItemCreated"])
	rawJSON := []byte(`{"itemID":"item-42","name":"Widget"}`)
	e := v1.Envelope{ID: 1, Kind: "ItemCreated"}

	err = handler(ctx, e, rawJSON)
	require.NoError(t, err)

	// Verify entity was stored
	entity, err := store.Get(ctx, "items", NewKey("item-42"))
	require.NoError(t, err)
	require.NotNil(t, entity)
	assert.Equal(t, "items", entity.Kind)
	assert.Equal(t, int64(1), entity.Version)

	var data testItemCreated
	err = json.Unmarshal(entity.Value, &data)
	require.NoError(t, err)
	assert.Equal(t, "item-42", data.ItemID)
	assert.Equal(t, "Widget", data.Name)
}

func TestIndexerHandlerNotifiesObserver(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	ctx := t.Context()
	stream, err := sys.Stream(ctx, "domain", "stream")
	require.NoError(t, err)

	proj := NewProjection("domain", "stream",
		OnKind("ItemCreated", func(_ context.Context, _ v1.Envelope, evt *testItemCreated, _ *ReduceContext) (*ReduceResult, error) {
			return &ReduceResult{
				Upserts: []Upsert{
					{Kind: "items", Key: NewKey(evt.ItemID), Value: evt},
				},
			}, nil
		}),
	)

	store := NewMemoryStore()
	notifier := NewNotifier()
	var notified []Change
	notifier.OnChange(func(_ context.Context, c Change) error {
		notified = append(notified, c)
		return nil
	})

	indexer := NewIndexer(proj, store, notifier, stream)
	handler := indexer.makeDispatchHandler("ItemCreated", proj.handlers["ItemCreated"])

	rawJSON := []byte(`{"itemID":"item-42","name":"Widget"}`)
	e := v1.Envelope{ID: 5, Kind: "ItemCreated"}
	err = handler(ctx, e, rawJSON)
	require.NoError(t, err)

	require.Len(t, notified, 1)
	assert.Equal(t, int64(5), notified[0].Version)
	assert.Len(t, notified[0].Upserts, 1)
}

func TestIndexerHandlerReadsPreviousState(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	ctx := t.Context()
	stream, err := sys.Stream(ctx, "domain", "stream")
	require.NoError(t, err)

	proj := NewProjection("domain", "stream",
		OnKind("ItemUpdated", func(_ context.Context, _ v1.Envelope, evt *testItemCreated, actx *ReduceContext) (*ReduceResult, error) {
			prev, err := actx.Get("items", NewKey(evt.ItemID))
			require.NoError(t, err)
			require.NotNil(t, prev)
			return &ReduceResult{
				Upserts: []Upsert{
					{Kind: "items", Key: NewKey(evt.ItemID), Value: evt},
				},
			}, nil
		}),
	)

	store := NewMemoryStore()
	// Pre-populate the store
	_, err = store.Persist(ctx, &ReduceResult{
		Upserts: []Upsert{
			{Kind: "items", Key: NewKey("item-42"), Value: testItemCreated{ItemID: "item-42", Name: "Old"}},
		},
	}, 1)
	require.NoError(t, err)

	notifier := NewNotifier()
	indexer := NewIndexer(proj, store, notifier, stream)
	handler := indexer.makeDispatchHandler("ItemUpdated", proj.handlers["ItemUpdated"])

	rawJSON := []byte(`{"itemID":"item-42","name":"Updated"}`)
	e := v1.Envelope{ID: 2, Kind: "ItemUpdated"}
	err = handler(ctx, e, rawJSON)
	require.NoError(t, err)

	// Verify entity was updated
	entity, err := store.Get(ctx, "items", NewKey("item-42"))
	require.NoError(t, err)
	require.NotNil(t, entity)
	assert.Equal(t, int64(2), entity.Version)
}

func TestIndexerNilResultAdvancesVersion(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	ctx := t.Context()
	stream, err := sys.Stream(ctx, "domain", "stream")
	require.NoError(t, err)

	proj := NewProjection("domain", "stream",
		OnKind("NoOp", func(_ context.Context, _ v1.Envelope, _ *testItemCreated, _ *ReduceContext) (*ReduceResult, error) {
			return nil, nil
		}),
	)

	store := NewMemoryStore()
	notifier := NewNotifier()
	indexer := NewIndexer(proj, store, notifier, stream)
	handler := indexer.makeDispatchHandler("NoOp", proj.handlers["NoOp"])

	var notified []Change
	unsubscribe := notifier.OnChange(func(_ context.Context, c Change) error {
		notified = append(notified, c)
		return nil
	})
	defer unsubscribe()

	err = handler(ctx, v1.Envelope{ID: 1}, []byte(`{}`))
	require.NoError(t, err)

	// The version advances even when the handler produced no mutations.
	assert.Equal(t, int64(1), store.Version())
	require.Len(t, notified, 1)
	assert.Equal(t, int64(1), notified[0].Version)
	assert.Empty(t, notified[0].Upserts)
	assert.Empty(t, notified[0].Deletes)
}
