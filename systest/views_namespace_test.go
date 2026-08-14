package systest

import (
	"context"
	"os"
	"testing"

	"github.com/meschbach/pgcqrs/pkg/indexer/views"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProjectionNamespaceIsolation verifies that two projections with the same
// ConsumerName on different {domain, stream} pairs get completely separate KV
// storage. This is a regression test for the bug where KV storage was namespaced
// by consumer name alone, causing cross-stream data contamination.
func TestProjectionNamespaceIsolation(t *testing.T) {
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

	handler := func(_ context.Context, _ v1.Envelope, evt *ItemCreated, _ *views.ReduceContext) (*views.ReduceResult, error) {
		return &views.ReduceResult{
			Upserts: []views.Upsert{
				{
					Kind:  "items",
					Key:   views.NewKey(evt.ItemID),
					Value: Item{ItemID: evt.ItemID, Name: evt.Name},
				},
			},
		}, nil
	}

	// Stream A: uses harness's default {domain, stream}
	domainA := harness.appName
	streamA := harness.streamName

	// Stream B: different domain AND different stream
	domainB := harness.appName + "-alt"
	streamB := harness.streamName + "-alt"

	streamBObj, err := harness.system.Stream(harness.ctx, domainB, streamB)
	require.NoError(t, err)

	// Both projections use the SAME ConsumerName to stress the namespace boundary
	projA := views.NewProjection(domainA, streamA,
		views.ConsumerName("namespace-test"),
		views.OnKind("ItemCreated", handler),
	)
	projB := views.NewProjection(domainB, streamB,
		views.ConsumerName("namespace-test"),
		views.OnKind("ItemCreated", handler),
	)

	clientA, err := connectProjection(harness.ctx, harness, projA)
	require.NoError(t, err)
	t.Cleanup(func() { closeClient(t, clientA) })

	clientB, err := connectProjection(harness.ctx, harness, projB)
	require.NoError(t, err)
	t.Cleanup(func() { closeClient(t, clientB) })

	// Submit event to stream A only
	result := harness.stream.MustSubmit(harness.ctx, "ItemCreated", ItemCreated{ItemID: "item-1", Name: "from-stream-a"})

	// Stream A's projection should have the entity
	valueA := requireEntityValue[Item](t, harness.ctx, clientA, "items", views.NewKey("item-1"), result.ID)
	assert.Equal(t, "from-stream-a", valueA.Name)

	// KEY ASSERTION: Stream B's projection must NOT see stream A's data
	entityB, resultB, err := clientB.Get(harness.ctx, "items", views.NewKey("item-1"))
	require.NoError(t, err)
	assert.Equal(t, views.StatusNotFound, resultB.Status,
		"stream B's projection must not see stream A's data — KV storage is not properly isolated by {domain, stream}")
	assert.Nil(t, entityB)

	// Submit event to stream B only
	streamBSubmit := streamBObj.MustSubmit(harness.ctx, "ItemCreated", ItemCreated{ItemID: "item-1", Name: "from-stream-b"})

	// Stream B's projection should now have its own entity
	valueB := requireEntityValue[Item](t, harness.ctx, clientB, "items", views.NewKey("item-1"), streamBSubmit.ID)
	assert.Equal(t, "from-stream-b", valueB.Name)

	// Stream A's projection should still have its original data (unchanged)
	valueA2 := requireEntityValue[Item](t, harness.ctx, clientA, "items", views.NewKey("item-1"), result.ID)
	assert.Equal(t, "from-stream-a", valueA2.Name)

	// Versions should be independently tracked on gRPC (memory transport has a
	// known limitation where event IDs are not propagated through TickWithID)
	if os.Getenv("PGCQRS_TEST_TRANSPORT") == "grpc" {
		// The pump advances consumer_positions via heartbeat after the KV write, so
		// poll for the version rather than asserting it immediately.
		versionA := waitForPositiveVersion(harness.ctx, t, clientA)
		versionB := waitForPositiveVersion(harness.ctx, t, clientB)
		assert.Positive(t, versionA, "stream A's projection should have a non-zero version")
		assert.Positive(t, versionB, "stream B's projection should have a non-zero version")
	}
}
