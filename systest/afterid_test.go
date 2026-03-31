package systest

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-faker/faker/v4"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	query2 "github.com/meschbach/pgcqrs/pkg/v1/query2"
	"github.com/stretchr/testify/require"
)

func TestAfterIDQueryFilter(t *testing.T) {
	t.Parallel()

	t.Run("AfterIDFilter", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()

		harness := setupHarnessT(t)
		defer harness.done()

		// Submit a series of events
		kind := faker.Word()
		eventIDs := make([]int64, 5)
		for i := 0; i < 5; i++ {
			submitted := harness.stream.MustSubmit(ctx, kind, map[string]string{"value": faker.Word()})
			eventIDs[i] = submitted.ID
		}

		// Query without After should return all
		var all []*v1.Envelope
		q := query2.NewQuery(harness.stream)
		q.OnKind(kind).Each(func(_ context.Context, env v1.Envelope, _ json.RawMessage) error {
			all = append(all, &env)
			return nil
		})
		err := q.StreamBatch(ctx)
		require.NoError(t, err)
		require.Len(t, all, 5)

		// Query with After(eventIDs[2]) should return events 3,4,5 (indices 3,4)
		afterID := eventIDs[2]
		var after []*v1.Envelope
		q2 := query2.NewQuery(harness.stream)
		q2.After(afterID)
		q2.OnKind(kind).Each(func(_ context.Context, env v1.Envelope, _ json.RawMessage) error {
			after = append(after, &env)
			return nil
		})
		err = q2.StreamBatch(ctx)
		require.NoError(t, err)
		require.Len(t, after, 2)
		if len(after) == 2 {
			require.Equal(t, eventIDs[3], after[0].ID)
			require.Equal(t, eventIDs[4], after[1].ID)
		}

		// After with ID beyond last event returns empty
		var results []*v1.Envelope
		q3 := query2.NewQuery(harness.stream)
		q3.After(eventIDs[4] + 1)
		q3.OnKind(kind).Each(func(_ context.Context, env v1.Envelope, _ json.RawMessage) error {
			results = append(results, &env)
			return nil
		})
		err = q3.StreamBatch(ctx)
		require.NoError(t, err)
		require.Empty(t, results)

		// After with ID less than first event returns all
		var results2 []*v1.Envelope
		q4 := query2.NewQuery(harness.stream)
		q4.After(eventIDs[0] - 1)
		q4.OnKind(kind).Each(func(_ context.Context, env v1.Envelope, _ json.RawMessage) error {
			results2 = append(results2, &env)
			return nil
		})
		err = q4.StreamBatch(ctx)
		require.NoError(t, err)
		require.Len(t, results2, 5)
	})

	t.Run("AfterIDWithSubsetMatch", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()

		harness := setupHarnessT(t)
		defer harness.done()

		kind := faker.Word()
		// Events 0, 1, 2 have same data, 3, 4 have different
		dataMatch := map[string]string{"key": "match"}
		dataOther := map[string]string{"key": "other"}

		eventIDs := make([]int64, 0, 6)
		for i := 0; i < 3; i++ {
			submitted := harness.stream.MustSubmit(ctx, kind, dataMatch)
			eventIDs = append(eventIDs, submitted.ID)
		}
		for i := 3; i < 5; i++ {
			submitted := harness.stream.MustSubmit(ctx, kind, dataOther)
			eventIDs = append(eventIDs, submitted.ID)
		}

		// Submit one more matching event
		submitted := harness.stream.MustSubmit(ctx, kind, dataMatch)
		eventIDs = append(eventIDs, submitted.ID) // ID 5

		// Query for subset after eventIDs[1] (ID 1)
		// Should return ID 2 and ID 5
		var results []*v1.Envelope
		q := query2.NewQuery(harness.stream)
		q.After(eventIDs[1])
		q.OnKind(kind).Subset(dataMatch).On(func(_ context.Context, env v1.Envelope, _ json.RawMessage) error {
			results = append(results, &env)
			return nil
		})
		err := q.StreamBatch(ctx)
		require.NoError(t, err)
		require.Len(t, results, 2)
		if len(results) == 2 {
			require.Equal(t, eventIDs[2], results[0].ID)
			require.Equal(t, eventIDs[5], results[1].ID)
		}
	})
}
