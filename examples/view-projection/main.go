package main

import (
	"context"
	"fmt"
	"time"

	"github.com/meschbach/pgcqrs/pkg/indexer/views"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
)

const app = "example.view-projection"
const stream = "inventory"

// ItemCreated represents an inventory item being created.
type ItemCreated struct {
	ItemID string `json:"itemID"`
	Name   string `json:"name"`
	Price  int    `json:"price"`
}

// ItemUpdated represents an inventory item being updated.
type ItemUpdated struct {
	ItemID string `json:"itemID"`
	Name   string `json:"name,omitempty"`
	Price  *int   `json:"price,omitempty"`
}

// ItemDeleted represents an inventory item being deleted.
type ItemDeleted struct {
	ItemID string `json:"itemID"`
}

// InventoryItem is the projected entity state.
type InventoryItem struct {
	ItemID string `json:"itemID"`
	Name   string `json:"name"`
	Price  int    `json:"price"`
}

func main() {
	ctx, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()

	// Create a projection with typed handlers
	proj := views.NewProjection(app, stream,
		views.OnKind("ItemCreated", func(ctx context.Context, e v1.Envelope, evt *ItemCreated, actx *views.ReduceContext) (*views.ReduceResult, error) {
			fmt.Printf("Processing ItemCreated: %s\n", evt.ItemID)
			return &views.ReduceResult{
				Upserts: []views.Upsert{
					{
						Kind: "items",
						Key:  views.NewKey(evt.ItemID),
						Value: InventoryItem{
							ItemID: evt.ItemID,
							Name:   evt.Name,
							Price:  evt.Price,
						},
					},
				},
			}, nil
		}),
		views.OnKind("ItemUpdated", func(ctx context.Context, e v1.Envelope, evt *ItemUpdated, actx *views.ReduceContext) (*views.ReduceResult, error) {
			fmt.Printf("Processing ItemUpdated: %s\n", evt.ItemID)
			// Read previous state
			prev, err := actx.Get("items", views.NewKey(evt.ItemID))
			if err != nil {
				return nil, err
			}
			if prev == nil {
				return nil, fmt.Errorf("item %s not found", evt.ItemID)
			}

			// Merge updates
			item := InventoryItem{ItemID: evt.ItemID}
			// Unmarshal previous state (simplified for example)
			item.Name = evt.Name
			if evt.Price != nil {
				item.Price = *evt.Price
			}

			return &views.ReduceResult{
				Upserts: []views.Upsert{
					{Kind: "items", Key: views.NewKey(evt.ItemID), Value: item},
				},
			}, nil
		}),
		views.OnKind("ItemDeleted", func(ctx context.Context, e v1.Envelope, evt *ItemDeleted, actx *views.ReduceContext) (*views.ReduceResult, error) {
			fmt.Printf("Processing ItemDeleted: %s\n", evt.ItemID)
			return &views.ReduceResult{
				Deletes: []views.Delete{
					{Kind: "items", Key: views.NewKey(evt.ItemID)},
				},
			}, nil
		}),
	)

	cfg := v1.NewConfig().LoadEnv()

	sys, err := cfg.SystemFromConfig()
	if err != nil {
		panic(err)
	}
	defer sys.Close()

	client, err := views.With(ctx, sys, proj)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	client.OnChange(func(_ context.Context, c views.Change) error {
		fmt.Printf("Change notification: version %d\n", c.Version)
		return nil
	})

	s, err := sys.Stream(ctx, app, stream)
	if err != nil {
		panic(err)
	}

	// Submit some events
	s.MustSubmit(ctx, "ItemCreated", ItemCreated{ItemID: "item-1", Name: "Widget", Price: 100})
	s.MustSubmit(ctx, "ItemCreated", ItemCreated{ItemID: "item-2", Name: "Gadget", Price: 200})

	// Wait for processing
	time.Sleep(500 * time.Millisecond)

	// Query the projected state
	entity, result, err := client.Get(ctx, "items", views.NewKey("item-1"))
	if err != nil {
		panic(err)
	}
	if result.Status == views.StatusOK && entity != nil {
		fmt.Printf("Retrieved item-1: version %d\n", entity.Version)
	}

	// Check version
	version, err := client.Version(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Current projection version: %d\n", version)

	fmt.Println("Success")
}
