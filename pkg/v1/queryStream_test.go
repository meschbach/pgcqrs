package v1

import (
	"context"
	"github.com/go-faker/faker/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestQueryStream(t *testing.T) {
	MemoryHarness(t, func(ctx context.Context, h Harness) {
		kind1 := faker.LastName()
		kind2 := faker.FirstName()
		kind3 := faker.Word()

		commonValue := faker.DomainName()

		h.stream.MustSubmit(ctx, kind1, PutEvent{Value: commonValue})
		targetEvent := h.stream.MustSubmit(ctx, kind2, PutEvent{Value: commonValue})
		h.stream.MustSubmit(ctx, kind3, PutEvent{Value: commonValue})
		h.stream.MustSubmit(ctx, faker.LastName(), PutEvent{Value: faker.Word()})

		var envelopes []Envelope
		var events []PutEvent
		query := h.stream.Query()
		query.WithKind(kind2).Match(putEventQuery{Value: commonValue}).On(EntityFunc[PutEvent](func(ctx context.Context, e Envelope, p PutEvent) {
			envelopes = append(envelopes, e)
			events = append(events, p)
		}))
		err := query.Stream(ctx)
		require.NoError(t, err)

		if assert.Len(t, envelopes, 1) {
			assert.Equal(t, targetEvent.ID, envelopes[0].ID)
		}
		if assert.Len(t, events, 1) {
			assert.Equal(t, PutEvent{Value: commonValue}, events[0])
		}
	})
}
