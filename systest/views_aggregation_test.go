package systest

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/meschbach/pgcqrs/pkg/indexer/views"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProjectionWithComplexHandlerLogic tests handlers with conditional logic,
// aggregations, and cross-entity references
func TestProjectionWithComplexHandlerLogic(t *testing.T) {
	t.Parallel()
	skipHTTP(t)
	harness := setupHarnessT(t)

	type OrderCreated struct {
		OrderID  string  `json:"orderID"`
		Customer string  `json:"customer"`
		Amount   float64 `json:"amount"`
	}

	type CustomerCreated struct {
		CustomerID string `json:"customerID"`
		Name       string `json:"name"`
	}

	type CustomerStats struct {
		CustomerID   string  `json:"customerID"`
		Name         string  `json:"name"`
		TotalOrders  int     `json:"totalOrders"`
		TotalSpent   float64 `json:"totalSpent"`
		AverageOrder float64 `json:"averageOrder"`
	}

	proj := views.NewProjection(harness.appName, harness.streamName,
		views.ConsumerName("customer-stats"),
		views.OnKind("CustomerCreated", func(_ context.Context, _ v1.Envelope, evt *CustomerCreated, _ *views.ReduceContext) (*views.ReduceResult, error) {
			return &views.ReduceResult{
				Upserts: []views.Upsert{
					{
						Kind: "customers",
						Key:  views.NewKey(evt.CustomerID),
						Value: CustomerStats{
							CustomerID:  evt.CustomerID,
							Name:        evt.Name,
							TotalOrders: 0,
							TotalSpent:  0,
						},
					},
				},
			}, nil
		}),
		views.OnKind("OrderCreated", func(_ context.Context, _ v1.Envelope, evt *OrderCreated, actx *views.ReduceContext) (*views.ReduceResult, error) {
			// Read existing customer stats
			existing, err := actx.Get("customers", views.NewKey(evt.Customer))
			if err != nil {
				return nil, err
			}
			if existing == nil {
				return nil, fmt.Errorf("customer %s not found", evt.Customer)
			}

			// Deserialize and update
			var stats CustomerStats
			if err := json.Unmarshal(existing.Value, &stats); err != nil {
				return nil, err
			}

			stats.TotalOrders++
			stats.TotalSpent += evt.Amount
			stats.AverageOrder = stats.TotalSpent / float64(stats.TotalOrders)

			return &views.ReduceResult{
				Upserts: []views.Upsert{
					{
						Kind:  "customers",
						Key:   views.NewKey(evt.Customer),
						Value: stats,
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
	stream.MustSubmit(harness.ctx, "CustomerCreated", CustomerCreated{CustomerID: "cust-1", Name: "Alice"})
	stream.MustSubmit(harness.ctx, "OrderCreated", OrderCreated{OrderID: "order-1", Customer: "cust-1", Amount: 100.0})
	last := stream.MustSubmit(harness.ctx, "OrderCreated", OrderCreated{OrderID: "order-2", Customer: "cust-1", Amount: 200.0})

	// Verify aggregated stats
	stats := requireEntityValue[CustomerStats](t, harness.ctx, client, "customers", views.NewKey("cust-1"), last.ID)
	assert.Equal(t, 2, stats.TotalOrders)
	assert.InEpsilon(t, 300.0, stats.TotalSpent, 0.001)
	assert.InEpsilon(t, 150.0, stats.AverageOrder, 0.001)
}
