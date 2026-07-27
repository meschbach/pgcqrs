package main

import (
	"context"
	"fmt"
	"time"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/meschbach/pgcqrs/pkg/indexer/views"
)

const app = "example.view-projection-watch"
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
			prev, err := actx.Get("items", views.NewKey(evt.ItemID))
			if err != nil {
				return nil, err
			}
			if prev == nil {
				return nil, fmt.Errorf("item %s not found", evt.ItemID)
			}

			item := InventoryItem{ItemID: evt.ItemID}
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
			return &views.ReduceResult{
				Deletes: []views.Delete{
					{Kind: "items", Key: views.NewKey(evt.ItemID)},
				},
			}, nil
		}),
	)

	cfg := v1.NewConfig().LoadEnv()
	
	var client *views.Client
	var sys *v1.System
	var err error
	
	switch cfg.TransportType {
	case v1.TransportTypeGRPC:
		client, err = views.Connect(ctx, cfg.ServiceURL, proj)
		if err != nil {
			panic(err)
		}
		sys, err = cfg.SystemFromConfig()
		if err != nil {
			panic(err)
		}
	case v1.TransportTypeMemory:
		sys, err = cfg.SystemFromConfig()
		if err != nil {
			panic(err)
		}
		client, err = views.ConnectMemory(ctx, sys.Transport, proj)
		if err != nil {
			panic(err)
		}
	case v1.TransportTypeHTTP:
		panic("view projections require gRPC transport; HTTP transport is not supported")
	default:
		panic(fmt.Sprintf("unsupported transport type: %s", cfg.TransportType))
	}
	defer client.Close()

	// Register for change notifications BEFORE submitting events
	client.OnChange(func(c views.Change) {
		fmt.Printf("Change notification: version %d\n", c.Version)
		fmt.Printf("  Upserts: %d, Deletes: %d\n", len(c.Upserts), len(c.Deletes))
		
		for _, u := range c.Upserts {
			fmt.Printf("    Upsert: kind=%s key=%v\n", u.Kind, u.Key.Parts())
		}
		for _, d := range c.Deletes {
			fmt.Printf("    Delete: kind=%s key=%v\n", d.Kind, d.Key.Parts())
		}
	})

	s, err := sys.Stream(ctx, app, stream)
	if err != nil {
		panic(err)
	}

	// Submit events - callbacks will fire as they're processed
	fmt.Println("Submitting ItemCreated events...")
	s.MustSubmit(ctx, "ItemCreated", ItemCreated{ItemID: "item-1", Name: "Widget", Price: 100})
	s.MustSubmit(ctx, "ItemCreated", ItemCreated{ItemID: "item-2", Name: "Gadget", Price: 200})

	// Wait for processing
	time.Sleep(500 * time.Millisecond)

	fmt.Println("\nSubmitting ItemUpdated event...")
	price := 150
	s.MustSubmit(ctx, "ItemUpdated", ItemUpdated{ItemID: "item-1", Name: "Widget v2", Price: &price})

	time.Sleep(500 * time.Millisecond)

	fmt.Println("\nSubmitting ItemDeleted event...")
	s.MustSubmit(ctx, "ItemDeleted", ItemDeleted{ItemID: "item-2"})

	time.Sleep(500 * time.Millisecond)

	fmt.Println("\nSuccess")
}
