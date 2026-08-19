package main

import (
	"context"
	"fmt"
	"time"

	"github.com/meschbach/pgcqrs/pkg/indexer/views"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
)

const app = "example.view-projection-versioned"
const stream = "inventory"

type ItemCreated struct {
	ItemID string `json:"itemID"`
	Name   string `json:"name"`
	Price  int    `json:"price"`
}

type ItemUpdated struct {
	ItemID string `json:"itemID"`
	Name   string `json:"name,omitempty"`
	Price  *int   `json:"price,omitempty"`
}

type ItemDeleted struct {
	ItemID string `json:"itemID"`
}

type InventoryItem struct {
	ItemID string `json:"itemID"`
	Name   string `json:"name"`
	Price  int    `json:"price"`
}

func main() {
	ctx, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()

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

	s, err := sys.Stream(ctx, app, stream)
	if err != nil {
		panic(err)
	}

	submitted1, err := s.Submit(ctx, "ItemCreated", ItemCreated{ItemID: "item-1", Name: "Widget", Price: 100})
	if err != nil {
		panic(err)
	}
	fmt.Printf("Submitted ItemCreated (event ID %d)\n", submitted1.ID)

	time.Sleep(200 * time.Millisecond)

	fmt.Println("\n--- UntilVersion: satisfied ---")
	entity, result, err := client.Get(ctx, "items", views.NewKey("item-1"),
		views.UntilVersion(submitted1.ID, 200*time.Millisecond))
	if err != nil {
		panic(err)
	}
	switch result.Status {
	case views.StatusOK:
		fmt.Printf("Got item-1 at version %d (target was %d)\n", entity.Version, submitted1.ID)
	case views.StatusTimeout:
		fmt.Printf("Timed out waiting for version %d\n", submitted1.ID)
	}

	fmt.Println("\n--- UntilVersion: timeout ---")
	futureVersion := submitted1.ID + 1000
	entity, result, err = client.Get(ctx, "items", views.NewKey("item-1"),
		views.UntilVersion(futureVersion, 100*time.Millisecond))
	if err != nil {
		panic(err)
	}
	switch result.Status {
	case views.StatusOK:
		fmt.Printf("Got item-1 at version %d\n", entity.Version)
	case views.StatusTimeout:
		fmt.Printf("Timed out waiting for version %d (current version: %d)\n", futureVersion, entity.Version)
	}

	submitted2, err := s.Submit(ctx, "ItemUpdated", ItemUpdated{ItemID: "item-1", Name: "Widget v2"})
	if err != nil {
		panic(err)
	}
	fmt.Printf("\nSubmitted ItemUpdated (event ID %d)\n", submitted2.ID)

	time.Sleep(200 * time.Millisecond)

	fmt.Println("\n--- After: satisfied ---")
	entity, result, err = client.Get(ctx, "items", views.NewKey("item-1"),
		views.After(submitted1.ID))
	if err != nil {
		panic(err)
	}
	switch result.Status {
	case views.StatusOK:
		fmt.Printf("Item-1 version %d is at or after %d\n", entity.Version, submitted1.ID)
	case views.StatusStale:
		fmt.Printf("Item-1 version %d is behind %d\n", entity.Version, submitted1.ID)
	}

	fmt.Println("\n--- After: stale ---")
	entity, result, err = client.Get(ctx, "items", views.NewKey("item-1"),
		views.After(submitted2.ID+100))
	if err != nil {
		panic(err)
	}
	switch result.Status {
	case views.StatusOK:
		fmt.Printf("Item-1 version %d is at or after %d\n", entity.Version, submitted2.ID+100)
	case views.StatusStale:
		fmt.Printf("Item-1 version %d is behind %d\n", entity.Version, submitted2.ID+100)
	}

	fmt.Println("\nSuccess")
}
