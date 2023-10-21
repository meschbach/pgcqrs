package systest

import (
	"context"
	"github.com/go-faker/faker/v4"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

type Example struct {
	Value string
}

// Tests the systems capability to use an `or` clause between two matches.
func TestMultiKindMatch(t *testing.T) {
	t.Run("With V1 Client, With two documents of the same kind", func(t *testing.T) {
		harness := setupHarness()
		ctx := harness.ctx
		t.Cleanup(func() {
			harness.done()
		})
		stream := harness.stream

		kind1 := faker.Name()
		kind2 := faker.Name()

		value1 := faker.Name()
		value1Sub, err := stream.Submit(ctx, kind1, Example{Value: value1})
		require.NoError(t, err)

		value2 := faker.Name()
		value2Sub, err := stream.Submit(ctx, kind1, Example{Value: value2})
		require.NoError(t, err)

		_, err = stream.Submit(ctx, kind2, Example{Value: value1})
		require.NoError(t, err)

		t.Run("When matching on kind only, Then it matches both", func(t *testing.T) {
			var matchedEnvelopes []v1.Envelope
			var matched []Example

			q := stream.Query()
			q.WithKind(kind1).On(v1.EntityFunc[Example](func(ctx context.Context, e v1.Envelope, entity Example) {
				matchedEnvelopes = append(matchedEnvelopes, e)
				matched = append(matched, entity)
			}))
			require.NoError(t, q.Stream(ctx))

			if assert.Len(t, matched, 2) {
				assert.Equal(t, value1, matched[0].Value)
				assert.Equal(t, value2, matched[1].Value)
			}
			if assert.Len(t, matchedEnvelopes, 2) {
				assert.Equal(t, value1Sub.ID, matchedEnvelopes[0].ID)
				assert.Equal(t, value2Sub.ID, matchedEnvelopes[1].ID)
			}
		})

		t.Run("When matching on matching kind and field, then it matches the document", func(t *testing.T) {
			var matched []matchedPair[Example]
			q := stream.Query()
			q.WithKind(kind1).Match(Example{Value: value1}).On(v1.EntityFunc[Example](func(ctx context.Context, e v1.Envelope, entity Example) {
				matched = append(matched, matchedPair[Example]{
					envelope: e,
					entity:   entity,
				})
			}))
			require.NoError(t, q.Stream(ctx))

			if assert.Len(t, matched, 1) {
				assert.Equal(t, value1Sub.ID, matched[0].envelope.ID)
				assert.Equal(t, value1, matched[0].entity.Value)
			}
		})
	})
}
