package v1

import (
	"context"
	"github.com/bxcodec/faker/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestKindFilter(t *testing.T) {
	MemoryHarness(t, func(ctx context.Context, h Harness) {
		fakeKind := faker.LastName()
		t1 := h.stream.MustSubmit(ctx, fakeKind, &PutEvent{Value: "test1"})
		t2 := h.stream.MustSubmit(ctx, fakeKind, &PutEvent{Value: "test2"})
		t3 := h.stream.MustSubmit(ctx, fakeKind, &PutEvent{Value: "test3"})
		h.stream.MustSubmit(ctx, faker.LastName(), &PutEvent{Value: "test1"})

		q := h.stream.Query()
		q.WithKind(fakeKind)
		results, err := q.Perform(ctx)
		require.NoError(t, err)
		if assert.Len(t, results.Envelopes(), 3) {
			assert.Equal(t, t1.ID, results.Envelopes()[0].ID)
			assert.Equal(t, t2.ID, results.Envelopes()[1].ID)
			assert.Equal(t, t3.ID, results.Envelopes()[2].ID)
		}
	})
}

func TestKindMatcherFilter(t *testing.T) {
	MemoryHarness(t, func(ctx context.Context, h Harness) {
		kind1 := faker.Word()
		targetCN := faker.FirstName()
		kind2 := faker.Word()

		h.stream.MustSubmit(ctx, kind1, &PutEvent{Value: faker.Word()})
		target := h.stream.MustSubmit(ctx, kind1, &PutEvent{Value: targetCN})
		h.stream.MustSubmit(ctx, kind2, &PutEvent{Value: faker.Word()})
		h.stream.MustSubmit(ctx, kind2, &PutEvent{Value: targetCN})

		q := h.stream.Query()
		q.WithKind(kind1).Eq("value", targetCN)

		results, err := q.Perform(ctx)
		require.NoError(t, err)
		if assert.Len(t, results.Envelopes(), 1) {
			assert.Equal(t, target.ID, results.Envelopes()[0].ID)
		}
	})
}

type Harness struct {
	appName    string
	streamName string

	system *System
	stream *Stream
}

func MemoryHarness(t *testing.T, perform func(ctx context.Context, h Harness)) {
	t.Parallel()

	ctx, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()

	harness := Harness{
		appName:    faker.Name(),
		streamName: faker.Name(),
	}
	mem := NewMemoryTransport()
	harness.system = NewSystem(mem)
	harness.stream = harness.system.MustStream(ctx, harness.appName, harness.streamName)

	perform(ctx, harness)
}
