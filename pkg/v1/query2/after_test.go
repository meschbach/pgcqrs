package query2

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/go-faker/faker/v4"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type afterTestEvent struct {
	Value string `json:"value"`
}

func TestAfterFilter(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	transport := v1.NewMemoryTransport()
	system := v1.NewSystem(transport)
	appName := faker.Name()
	streamName := faker.Name()
	stream := system.MustStream(ctx, appName, streamName)

	// Submit a series of events
	kind := faker.Word()
	events := make([]*v1.Submitted, 5)
	for i := 0; i < 5; i++ {
		events[i] = stream.MustSubmit(ctx, kind, &afterTestEvent{Value: faker.Word()})
	}

	// Query without After should return all
	var all []*v1.Envelope
	q := NewQuery(stream)
	q.OnKind(kind).Each(func(_ context.Context, env v1.Envelope, _ json.RawMessage) error {
		all = append(all, &env)
		return nil
	})
	err := q.StreamBatch(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 5)

	// Query with After(events[2].ID) should return events 3,4,5
	afterID := events[2].ID
	var after []*v1.Envelope
	q2 := NewQuery(stream)
	q2.After(afterID)
	q2.OnKind(kind).Each(func(_ context.Context, env v1.Envelope, _ json.RawMessage) error {
		after = append(after, &env)
		return nil
	})
	err = q2.StreamBatch(ctx)
	require.NoError(t, err)
	assert.Len(t, after, 2)
	if len(after) == 2 {
		assert.Equal(t, events[3].ID, after[0].ID)
		assert.Equal(t, events[4].ID, after[1].ID)
	}
}

func TestAfterEdgeCases(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	transport := v1.NewMemoryTransport()
	system := v1.NewSystem(transport)
	appName := faker.Name()
	streamName := faker.Name()
	stream := system.MustStream(ctx, appName, streamName)

	kind := faker.Word()
	event1 := stream.MustSubmit(ctx, kind, &afterTestEvent{Value: faker.Word()})

	// After with ID beyond last event returns empty
	var results []*v1.Envelope
	q := NewQuery(stream)
	q.After(event1.ID + 1)
	q.OnKind(kind).Each(func(_ context.Context, env v1.Envelope, _ json.RawMessage) error {
		results = append(results, &env)
		return nil
	})
	err := q.StreamBatch(ctx)
	require.NoError(t, err)
	assert.Empty(t, results)

	// After with ID less than first event returns all
	q2 := NewQuery(stream)
	q2.After(event1.ID - 1)
	q2.OnKind(kind).Each(func(_ context.Context, env v1.Envelope, _ json.RawMessage) error {
		results = append(results, &env)
		return nil
	})
	err = q2.StreamBatch(ctx)
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

func TestAfterWithMultipleKinds(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	transport := v1.NewMemoryTransport()
	system := v1.NewSystem(transport)
	appName := faker.Name()
	streamName := faker.Name()
	stream := system.MustStream(ctx, appName, streamName)

	kind1 := faker.Word()
	kind2 := faker.Word()

	// Submit events of both kinds interleaved
	stream.MustSubmit(ctx, kind1, &afterTestEvent{Value: "a"}) // ID 0
	stream.MustSubmit(ctx, kind2, &afterTestEvent{Value: "b"}) // ID 1
	stream.MustSubmit(ctx, kind1, &afterTestEvent{Value: "c"}) // ID 2
	stream.MustSubmit(ctx, kind2, &afterTestEvent{Value: "d"}) // ID 3

	// Query for kind1 after event ID 1 should get event ID 2 only
	var results []*v1.Envelope
	q := NewQuery(stream)
	q.After(1)
	q.OnKind(kind1).Each(func(_ context.Context, env v1.Envelope, _ json.RawMessage) error {
		results = append(results, &env)
		return nil
	})
	err := q.StreamBatch(ctx)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	if len(results) == 1 {
		assert.Equal(t, int64(2), results[0].ID)
	}
}
