package systest

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/meschbach/pgcqrs/pkg/indexer/views"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProjectionCrossEntityReferences tests handlers reading entities from different kinds
func TestProjectionCrossEntityReferences(t *testing.T) {
	t.Parallel()
	skipHTTP(t)
	harness := setupHarnessT(t)

	type ProductCreated struct {
		ProductID string  `json:"productID"`
		Name      string  `json:"name"`
		Price     float64 `json:"price"`
	}

	type CartItemAdded struct {
		CartID    string `json:"cartID"`
		ProductID string `json:"productID"`
		Quantity  int    `json:"quantity"`
	}

	type Product struct {
		ProductID string  `json:"productID"`
		Name      string  `json:"name"`
		Price     float64 `json:"price"`
	}

	type Item struct {
		ProductID   string  `json:"productID"`
		ProductName string  `json:"productName"`
		Price       float64 `json:"price"`
		Quantity    int     `json:"quantity"`
		Subtotal    float64 `json:"subtotal"`
	}

	type CartSummary struct {
		CartID     string  `json:"cartID"`
		Items      []Item  `json:"items"`
		TotalValue float64 `json:"totalValue"`
	}

	proj := views.NewProjection(harness.appName, harness.streamName,
		views.ConsumerName("cart-summary"),
		//nolint:dupl // Similar structure to ItemCreated handler but with different types (ProductCreated vs ItemCreated)
		views.OnKind("ProductCreated", func(_ context.Context, _ v1.Envelope, evt *ProductCreated, _ *views.ReduceContext) (*views.ReduceResult, error) {
			return &views.ReduceResult{
				Upserts: []views.Upsert{
					{
						Kind: "products",
						Key:  views.NewKey(evt.ProductID),
						Value: Product{
							ProductID: evt.ProductID,
							Name:      evt.Name,
							Price:     evt.Price,
						},
					},
				},
			}, nil
		}),
		views.OnKind("CartItemAdded", func(_ context.Context, _ v1.Envelope, evt *CartItemAdded, actx *views.ReduceContext) (*views.ReduceResult, error) {
			// Read product information
			productEntity, err := actx.Get("products", views.NewKey(evt.ProductID))
			if err != nil {
				return nil, err
			}
			if productEntity == nil {
				return nil, fmt.Errorf("product %s not found", evt.ProductID)
			}

			var product Product
			if err := json.Unmarshal(productEntity.Value, &product); err != nil {
				return nil, err
			}

			// Read existing cart
			cartEntity, err := actx.Get("carts", views.NewKey(evt.CartID))
			if err != nil {
				return nil, err
			}

			var cart CartSummary
			if cartEntity != nil {
				if err := json.Unmarshal(cartEntity.Value, &cart); err != nil {
					return nil, err
				}
			} else {
				cart = CartSummary{CartID: evt.CartID}
			}

			// Add item to cart
			item := Item{
				ProductID:   evt.ProductID,
				ProductName: product.Name,
				Price:       product.Price,
				Quantity:    evt.Quantity,
				Subtotal:    product.Price * float64(evt.Quantity),
			}
			cart.Items = append(cart.Items, item)
			cart.TotalValue += item.Subtotal

			return &views.ReduceResult{
				Upserts: []views.Upsert{
					{
						Kind:  "carts",
						Key:   views.NewKey(evt.CartID),
						Value: cart,
					},
				},
			}, nil
		}),
	)

	client, err := connectProjection(harness.ctx, harness, proj)
	require.NoError(t, err)
	t.Cleanup(func() { closeClient(t, client) })

	// Submit events
	stream := harness.system.MustStream(harness.ctx, harness.appName, harness.streamName)
	stream.MustSubmit(harness.ctx, "ProductCreated", ProductCreated{ProductID: "prod-1", Name: "Widget", Price: 10.0})
	stream.MustSubmit(harness.ctx, "ProductCreated", ProductCreated{ProductID: "prod-2", Name: "Gadget", Price: 20.0})
	stream.MustSubmit(harness.ctx, "CartItemAdded", CartItemAdded{CartID: "cart-1", ProductID: "prod-1", Quantity: 2})
	last := stream.MustSubmit(harness.ctx, "CartItemAdded", CartItemAdded{CartID: "cart-1", ProductID: "prod-2", Quantity: 1})

	// Wait for the projection to process all events
	cart := requireEntityValue[CartSummary](t, harness.ctx, client, "carts", views.NewKey("cart-1"), last.ID, 5*time.Second)
	assert.Equal(t, "cart-1", cart.CartID)
	assert.Len(t, cart.Items, 2)
	assert.InEpsilon(t, 40.0, cart.TotalValue, 0.001) // 2*10 + 1*20
}
