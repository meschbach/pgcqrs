package batchr2

import (
	"context"
	"github.com/bxcodec/faker/v3"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/meschbach/pgcqrs/pkg/v1/query2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

type event[V any] struct {
	envelope v1.Envelope
	event    V
}

type resultAccumulator struct {
	events []event[SimpleDocument]
}

func (r *resultAccumulator) OnEvent() v1.OnStreamQueryResult {
	return v1.EntityFunc[SimpleDocument](func(ctx context.Context, e v1.Envelope, entity SimpleDocument) {
		r.events = append(r.events, event[SimpleDocument]{
			envelope: e,
			event:    entity,
		})
	})
}

func TestIDMatch(t *testing.T) {
	_, ctx, stream := setupHarnessT(t)

	t.Run("Given a stream with several events, When querying for a single ID, then that ID is returned", func(t *testing.T) {
		kind := faker.FirstName()
		doc2 := SimpleDocument{StringValue: faker.Name()}
		_, err := stream.Submit(ctx, kind, SimpleDocument{StringValue: faker.Name()})
		require.NoError(t, err)
		submitted, err := stream.Submit(ctx, kind, doc2)
		require.NoError(t, err)

		_, err = stream.Submit(ctx, kind, SimpleDocument{StringValue: faker.Name()})
		require.NoError(t, err)

		accumulator := &resultAccumulator{}
		q := query2.NewQuery(stream)
		q.OnID(submitted.ID).On(accumulator.OnEvent())
		require.NoError(t, q.StreamBatch(ctx))

		if assert.Len(t, accumulator.events, 1) {
			assert.Equal(t, submitted.ID, accumulator.events[0].envelope.ID)
			assert.Equal(t, kind, accumulator.events[0].envelope.Kind)
			assert.Equal(t, doc2.StringValue, accumulator.events[0].event.StringValue)
		}
	})
}
