package inmemory

import (
	"context"
	"github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

const (
	EventExampleA = "events.a"
	EventExampleB = "events.b"
)

type ExampleEvent struct {
	Value string `json:"value"`
}

func TestStoresEvents(t *testing.T) {
	t.Parallel()

	ctx, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()
	storage := v1.NewMemoryTransport()
	system := v1.NewSystem(storage)
	stream := system.MustStream(ctx, "test-app", "test-stream")

	a := stream.MustSubmit(ctx, EventExampleA, ExampleEvent{Value: EventExampleA})
	b := stream.MustSubmit(ctx, EventExampleA, ExampleEvent{Value: EventExampleA})

	envelopes := stream.MustAll(ctx)
	if assert.Len(t, envelopes, 2) {
		assert.Equal(t, int64(a.ID), envelopes[0].ID)
		assert.Equal(t, int64(b.ID), envelopes[1].ID)
	}
}

func TestRecallsEvents(t *testing.T) {
	t.Parallel()

	ctx, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()
	storage := v1.NewMemoryTransport()
	system := v1.NewSystem(storage)
	stream := system.MustStream(ctx, "last dance", "just like you")

	a := stream.MustSubmit(ctx, EventExampleA, ExampleEvent{Value: EventExampleA})
	b := stream.MustSubmit(ctx, EventExampleA, ExampleEvent{Value: EventExampleA})

	envelopes := stream.MustAll(ctx)
	if assert.Len(t, envelopes, 2) {
		var eventA ExampleEvent
		stream.MustGet(ctx, a.ID, &eventA)
		assert.Equal(t, EventExampleA, eventA.Value)

		var eventB ExampleEvent
		stream.MustGet(ctx, b.ID, &eventB)
		assert.Equal(t, EventExampleA, eventA.Value)
	}
}
