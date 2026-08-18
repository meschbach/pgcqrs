package views_test

import (
	"context"
	"fmt"
	"time"

	"github.com/meschbach/pgcqrs/pkg/indexer/views"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
)

// ItemCreated represents an inventory item being created.
type ItemCreated struct {
	ItemID string `json:"itemID"`
	Name   string `json:"name"`
	Price  int    `json:"price"`
}

// InventoryItem is the projected entity state.
type InventoryItem struct {
	ItemID string `json:"itemID"`
	Name   string `json:"name"`
	Price  int    `json:"price"`
}

func ExampleNewProjection() {
	ctx := context.Background()

	// Create a projection with typed handlers
	proj := views.NewProjection("inventory", "items",
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
	)

	// Create in-memory client
	sys := v1.NewSystem(v1.NewMemoryTransport())
	defer sys.Close()

	client, err := views.With(ctx, sys, proj)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	// Get the stream and submit an event
	stream, err := sys.Stream(ctx, "inventory", "items")
	if err != nil {
		panic(err)
	}
	stream.MustSubmit(ctx, "ItemCreated", ItemCreated{ItemID: "item-42", Name: "Widget", Price: 100})

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Query entity
	entity, result, err := client.Get(ctx, "items", views.NewKey("item-42"))
	if err != nil {
		panic(err)
	}
	if result.Status == views.StatusOK {
		fmt.Printf("Found item: %s\n", entity.Key.Parts()[0])
	}

	// Output:
	// Found item: item-42
}

func ExampleClient_Get() {
	ctx := context.Background()

	// Setup projection and client
	proj := views.NewProjection("inventory", "items",
		views.OnKind("ItemCreated", func(ctx context.Context, e v1.Envelope, evt *ItemCreated, actx *views.ReduceContext) (*views.ReduceResult, error) {
			return &views.ReduceResult{
				Upserts: []views.Upsert{
					{Kind: "items", Key: views.NewKey(evt.ItemID), Value: evt},
				},
			}, nil
		}),
	)

	sys := v1.NewSystem(v1.NewMemoryTransport())
	defer sys.Close()

	client, err := views.With(ctx, sys, proj)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	// Submit event
	stream, _ := sys.Stream(ctx, "inventory", "items")
	stream.MustSubmit(ctx, "ItemCreated", ItemCreated{ItemID: "item-1", Name: "Widget", Price: 100})
	time.Sleep(100 * time.Millisecond)

	// Basic Get
	entity, result, err := client.Get(ctx, "items", views.NewKey("item-1"))
	if err != nil {
		panic(err)
	}
	if result.Status == views.StatusOK {
		fmt.Printf("Status: OK, Version: %d\n", entity.Version)
	}

	// Get with version constraint
	_, result, err = client.Get(ctx, "items", views.NewKey("item-1"), views.After(5))
	if err != nil {
		panic(err)
	}
	if result.Status == views.StatusStale {
		fmt.Println("Status: Stale")
	}

	// Output:
	// Status: OK, Version: 0
	// Status: Stale
}

func ExampleClient_OnChange() {
	ctx := context.Background()

	// Setup
	proj := views.NewProjection("inventory", "items",
		views.OnKind("ItemCreated", func(ctx context.Context, e v1.Envelope, evt *ItemCreated, actx *views.ReduceContext) (*views.ReduceResult, error) {
			return &views.ReduceResult{
				Upserts: []views.Upsert{
					{Kind: "items", Key: views.NewKey(evt.ItemID), Value: evt},
				},
			}, nil
		}),
	)

	sys := v1.NewSystem(v1.NewMemoryTransport())
	defer sys.Close()

	client, err := views.With(ctx, sys, proj)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	// Register change callback
	unsubscribe := client.OnChange(func(ctx context.Context, c views.Change) error {
		fmt.Printf("Version %d: %d upserts\n", c.Version, len(c.Upserts))
		return nil
	})
	defer unsubscribe()

	// Submit event to trigger notification
	stream, _ := sys.Stream(ctx, "inventory", "items")
	stream.MustSubmit(ctx, "ItemCreated", ItemCreated{ItemID: "item-1", Name: "Widget", Price: 100})

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Output:
	// Version 0: 1 upserts
}

func ExampleUntilVersion() {
	ctx := context.Background()

	// Setup
	proj := views.NewProjection("inventory", "items",
		views.OnKind("ItemCreated", func(ctx context.Context, e v1.Envelope, evt *ItemCreated, actx *views.ReduceContext) (*views.ReduceResult, error) {
			return &views.ReduceResult{
				Upserts: []views.Upsert{
					{Kind: "items", Key: views.NewKey(evt.ItemID), Value: evt},
				},
			}, nil
		}),
	)

	sys := v1.NewSystem(v1.NewMemoryTransport())
	defer sys.Close()

	client, err := views.With(ctx, sys, proj)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	// Submit events
	stream, _ := sys.Stream(ctx, "inventory", "items")
	stream.MustSubmit(ctx, "ItemCreated", ItemCreated{ItemID: "item-1", Name: "Widget", Price: 100})
	stream.MustSubmit(ctx, "ItemCreated", ItemCreated{ItemID: "item-2", Name: "Gadget", Price: 200})

	// Wait for version 1 (second event) with timeout
	entity, result, err := client.Get(ctx, "items", views.NewKey("item-2"), views.UntilVersion(1, 200*time.Millisecond))
	if err != nil {
		panic(err)
	}
	if result.Status == views.StatusOK {
		fmt.Printf("Got entity at version %d\n", entity.Version)
	}

	// Try to wait for version 999 (won't happen)
	_, result, err = client.Get(ctx, "items", views.NewKey("item-2"), views.UntilVersion(999, 100*time.Millisecond))
	if err != nil {
		panic(err)
	}
	if result.Status == views.StatusTimeout {
		fmt.Println("Timeout waiting for version 999")
	}

	// Output:
	// Got entity at version 1
	// Timeout waiting for version 999
}
