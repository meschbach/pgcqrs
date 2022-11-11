package systest

import (
	"context"
	"github.com/bxcodec/faker/v3"
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
	t.Run("With v1 Client matching multiple kinds", func(t *testing.T) {
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

		t.Run("Able to match correct records on just match", func(t *testing.T) {
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
	})
}
