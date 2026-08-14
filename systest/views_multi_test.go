package systest

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/meschbach/pgcqrs/pkg/indexer"
	"github.com/meschbach/pgcqrs/pkg/indexer/views"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMultipleProjectionsOnSameStream verifies that multiple projections can
// consume the same stream independently, each maintaining their own state
func TestMultipleProjectionsOnSameStream(t *testing.T) {
	t.Parallel()
	skipHTTP(t)
	harness := setupHarnessT(t)

	type ItemCreated struct {
		ItemID string `json:"itemID"`
		Name   string `json:"name"`
		Price  int    `json:"price"`
	}

	type Item struct {
		ItemID string `json:"itemID"`
		Name   string `json:"name"`
		Price  int    `json:"price"`
	}

	// Create two different projections on the same stream
	proj1 := views.NewProjection(harness.appName, harness.streamName,
		views.ConsumerName("projection-1"),
		//nolint:dupl // Similar structure to ProductCreated handler but with different types (ItemCreated vs ProductCreated)
		views.OnKind("ItemCreated", func(_ context.Context, _ v1.Envelope, evt *ItemCreated, _ *views.ReduceContext) (*views.ReduceResult, error) {
			return &views.ReduceResult{
				Upserts: []views.Upsert{
					{
						Kind: "items",
						Key:  views.NewKey(evt.ItemID),
						Value: Item{
							ItemID: evt.ItemID,
							Name:   evt.Name,
							Price:  evt.Price,
						},
					},
				},
			}, nil
		}),
	)

	proj2 := views.NewProjection(harness.appName, harness.streamName,
		views.ConsumerName("projection-2"),
		views.OnKind("ItemCreated", func(_ context.Context, _ v1.Envelope, evt *ItemCreated, _ *views.ReduceContext) (*views.ReduceResult, error) {
			return &views.ReduceResult{
				Upserts: []views.Upsert{
					{
						Kind: "items",
						Key:  views.NewKey(evt.ItemID),
						Value: Item{
							ItemID: evt.ItemID,
							Name:   evt.Name,
							Price:  evt.Price * 2, // Different transformation
						},
					},
				},
			}, nil
		}),
	)

	// Connect both projections
	client1, err := connectProjection(harness.ctx, harness, proj1)
	require.NoError(t, err)
	t.Cleanup(func() { closeClient(t, client1) })

	client2, err := connectProjection(harness.ctx, harness, proj2)
	require.NoError(t, err)
	t.Cleanup(func() { closeClient(t, client2) })

	// Wait for both projections to reach Watching state
	waitForState(t, client1, indexer.PumpStateWatching)
	waitForState(t, client2, indexer.PumpStateWatching)

	// Submit events
	stream := harness.system.MustStream(harness.ctx, harness.appName, harness.streamName)
	stream.MustSubmit(harness.ctx, "ItemCreated", ItemCreated{ItemID: "item-1", Name: "Widget", Price: 100})
	last := stream.MustSubmit(harness.ctx, "ItemCreated", ItemCreated{ItemID: "item-2", Name: "Gadget", Price: 200})

	// Verify both projections processed the events
	value1 := requireEntityValue[Item](t, harness.ctx, client1, "items", views.NewKey("item-1"), last.ID)
	value2 := requireEntityValue[Item](t, harness.ctx, client2, "items", views.NewKey("item-1"), last.ID)

	// proj1 should have price=100, proj2 should have price=200
	assert.Equal(t, 100, value1.Price)
	assert.Equal(t, 200, value2.Price)
}

// TestConcurrentReadersDuringUpdates verifies that readers get consistent
// snapshots while the projection is actively updating
func TestConcurrentReadersDuringUpdates(t *testing.T) {
	t.Parallel()
	skipHTTP(t)
	harness := setupHarnessT(t)

	type ItemCreated struct {
		ItemID string `json:"itemID"`
		Name   string `json:"name"`
	}

	type Item struct {
		ItemID string `json:"itemID"`
		Name   string `json:"name"`
	}

	proj := views.NewProjection(harness.appName, harness.streamName,
		views.ConsumerName("concurrent-test"),
		views.OnKind("ItemCreated", func(_ context.Context, _ v1.Envelope, evt *ItemCreated, _ *views.ReduceContext) (*views.ReduceResult, error) {
			return &views.ReduceResult{
				Upserts: []views.Upsert{
					{
						Kind: "items",
						Key:  views.NewKey(evt.ItemID),
						Value: Item{
							ItemID: evt.ItemID,
							Name:   evt.Name,
						},
					},
				},
			}, nil
		}),
	)

	client, err := connectProjection(harness.ctx, harness, proj)
	require.NoError(t, err)
	t.Cleanup(func() { closeClient(t, client) })

	// Wait for projection to reach Watching state
	waitForState(t, client, indexer.PumpStateWatching)

	// Start concurrent readers
	var wg sync.WaitGroup
	var readErrors int32
	var successfulReads int32

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(harness.ctx, 5*time.Second)
			defer cancel()

			for j := 0; j < 10; j++ {
				_, _, err := client.Get(ctx, "items", views.NewKey("item-1"))
				if err != nil {
					atomic.AddInt32(&readErrors, 1)
				} else {
					atomic.AddInt32(&successfulReads, 1)
				}
				time.Sleep(10 * time.Millisecond)
			}
		}()
	}

	// Submit events while readers are active
	stream := harness.system.MustStream(harness.ctx, harness.appName, harness.streamName)
	for i := 0; i < 20; i++ {
		stream.MustSubmit(harness.ctx, "ItemCreated", ItemCreated{
			ItemID: fmt.Sprintf("item-%d", i),
			Name:   fmt.Sprintf("Item %d", i),
		})
		time.Sleep(5 * time.Millisecond)
	}

	wg.Wait()

	// Verify no read errors occurred
	assert.Equal(t, int32(0), atomic.LoadInt32(&readErrors))
	assert.Positive(t, atomic.LoadInt32(&successfulReads))
}
