package systest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/meschbach/pgcqrs/pkg/indexer/views"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/require"
)

// TestProjectionErrorRecovery tests what happens when handlers fail
// Note: Current behavior is that handler errors stop the projection processing.
// This test documents this behavior - it's important for production systems to know
// that handler bugs will stop the projection, not silently skip events.
func TestProjectionErrorRecovery(t *testing.T) {
	t.Parallel()
	skipHTTP(t)
	harness := setupHarnessT(t)

	type OrderCreated struct {
		OrderID  string  `json:"orderID"`
		Customer string  `json:"customer"`
		Amount   float64 `json:"amount"`
	}

	type Order struct {
		OrderID  string  `json:"orderID"`
		Customer string  `json:"customer"`
		Amount   float64 `json:"amount"`
	}

	// Handler that fails for certain conditions
	proj := views.NewProjection(harness.appName, harness.streamName,
		views.ConsumerName("error-test"),
		views.OnKind("OrderCreated", func(_ context.Context, _ v1.Envelope, evt *OrderCreated, _ *views.ReduceContext) (*views.ReduceResult, error) {
			if evt.Amount < 0 {
				return nil, fmt.Errorf("invalid amount: %f", evt.Amount)
			}
			if evt.Customer == "" {
				return nil, fmt.Errorf("missing customer")
			}

			return &views.ReduceResult{
				Upserts: []views.Upsert{
					{
						Kind: "orders",
						Key:  views.NewKey(evt.OrderID),
						Value: Order{
							OrderID:  evt.OrderID,
							Customer: evt.Customer,
							Amount:   evt.Amount,
						},
					},
				},
			}, nil
		}),
	)

	client, err := connectProjection(harness.ctx, t, harness, proj)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, client.Close())
	}()

	// Submit valid event
	stream := harness.system.MustStream(harness.ctx, harness.appName, harness.streamName)
	order1Submit := stream.MustSubmit(harness.ctx, "OrderCreated", OrderCreated{OrderID: "order-1", Customer: "cust-1", Amount: 100.0})

	// Verify valid event was processed
	requireEntityValue[Order](t, harness.ctx, client, "orders", views.NewKey("order-1"), order1Submit.ID)

	// Submit invalid event (should fail in handler and stop processing)
	stream.MustSubmit(harness.ctx, "OrderCreated", OrderCreated{OrderID: "order-2", Customer: "cust-2", Amount: -50.0})

	// Wait for processing
	time.Sleep(500 * time.Millisecond)

	// Verify invalid event was not processed
	entity, result, err := client.Get(harness.ctx, "orders", views.NewKey("order-2"))
	require.NoError(t, err)
	require.Equal(t, views.StatusNotFound, result.Status)
	require.Nil(t, entity)

	// Submit another valid event - it will NOT be processed because the projection stopped
	// This documents the current behavior: handler errors stop the projection
	stream.MustSubmit(harness.ctx, "OrderCreated", OrderCreated{OrderID: "order-3", Customer: "cust-3", Amount: 200.0})

	// Wait for processing
	time.Sleep(500 * time.Millisecond)

	// Verify the new event was NOT processed (projection stopped after error)
	entity, result, err = client.Get(harness.ctx, "orders", views.NewKey("order-3"))
	require.NoError(t, err)
	require.Equal(t, views.StatusNotFound, result.Status, "Expected order-3 to not be processed because projection stopped after handler error")
	require.Nil(t, entity)
}
