package systest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/meschbach/pgcqrs/pkg/indexer/views"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// connectProjection creates a projection client using the appropriate method based on transport
func connectProjection(ctx context.Context, _ *testing.T, harness *harness, proj *views.Projection) (*views.Client, error) {
	transport := os.Getenv("PGCQRS_TEST_TRANSPORT")
	if transport == "grpc" {
		return views.Connect(ctx, harness.serviceURL, proj)
	}
	// For memory transport, use the same transport instance
	return views.ConnectMemory(ctx, harness.transport, proj)
}

// TestGRPCWatchDuplicateEvents is a minimal test to demonstrate the gRPC Watch bug
// where events are delivered multiple times, causing duplicate processing.
func TestGRPCWatchDuplicateEvents(t *testing.T) {
	t.Parallel()
	skipHTTP(t)
	harness := setupHarnessT(t)

	type CounterEvent struct {
		ID string `json:"id"`
	}

	type Counter struct {
		Count    int      `json:"count"`
		EventIDs []string `json:"eventIDs"` // Track which events were processed
	}

	type onKindData struct {
		callCount int
	}

	onKindCallData := &onKindData{}

	// Sanity check -- no events exist on this domain + stream
	events, err := harness.stream.All(t.Context())
	require.NoError(t, err)
	require.Empty(t, events, "test assumes stream is empty")

	proj := views.NewProjection(harness.appName, harness.streamName,
		views.ConsumerName("counter-test"),
		views.OnKind("CounterEvent", func(_ context.Context, e v1.Envelope, evt *CounterEvent, actx *views.ReduceContext) (*views.ReduceResult, error) {
			t.Logf("Received Event %d\n", e.ID)
			onKindCallData.callCount++
			// Read current counter
			existing, err := actx.Get("counters", views.NewKey("main"))
			if err != nil {
				return nil, err
			}

			var counter Counter
			if existing != nil {
				if err := json.Unmarshal(existing.Value, &counter); err != nil {
					return nil, err
				}
			}
			t.Logf("Counter view: %#v", counter)

			// Increment counter and track event ID
			counter.Count++
			counter.EventIDs = append(counter.EventIDs, evt.ID)

			return &views.ReduceResult{
				Upserts: []views.Upsert{
					{
						Kind:  "counters",
						Key:   views.NewKey("main"),
						Value: counter,
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

	// basic sanity check before we continue -- we expect 0 events on startup
	assert.Equal(t, 0, onKindCallData.callCount, "no prior events exist")

	// Submit exactly 5 events
	stream := harness.system.MustStream(harness.ctx, harness.appName, harness.streamName)
	var lastEventID int64
	for i := range 5 {
		sub := stream.MustSubmit(harness.ctx, "CounterEvent", CounterEvent{ID: fmt.Sprintf("event-%d", i)})
		lastEventID = sub.ID
	}

	// Wait for the projection to process all events
	counter := requireEntityValue[Counter](t, harness.ctx, client, "counters", views.NewKey("main"), lastEventID, 5*time.Second)

	// Did we get our 5 events?
	assert.Equal(t, 5, onKindCallData.callCount, "projection just called 5 times")

	// Log detailed information about what was processed
	t.Logf("Expected 5 events, got %d", counter.Count)
	t.Logf("Event IDs processed: %v", counter.EventIDs)

	// Count occurrences of each event ID
	occurrences := make(map[string]int)
	for _, id := range counter.EventIDs {
		occurrences[id]++
	}

	t.Logf("Event ID occurrences:")
	for id, count := range occurrences {
		t.Logf("  %s: %d times", id, count)
	}

	assert.Equal(t, 5, counter.Count, "Expected exactly 5 events to be processed, got %d (indicates duplicate event delivery)", counter.Count)
}
