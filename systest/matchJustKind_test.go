package systest

import (
	"context"
	"github.com/bxcodec/faker/v3"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

// Tests the systems capability to use an `or` clause between two matches.
func TestMultiKindMatch(t *testing.T) {
	t.Run("With v1 Client matching multiple kinds", func(t *testing.T) {
		ctx, done := context.WithCancel(context.Background())
		defer done()
		url, hasURL := os.LookupEnv("PGCQRS_TEST_URL")
		appBase, hasAppBase := os.LookupEnv("PGCQRS_TEST_APP_BASE")

		if !hasURL || !hasAppBase {
			t.Fatalf("Requires env PGCQRS_TEST_URL and PGCQRS_TEST_APP_BASE but is missing at least one")
			return
		}

		config := v1.Config{
			TransportType: v1.TransportTypeHTTP,
			ServiceURL:    url,
		}
		system, err := config.SystemFromConfig()
		require.NoError(t, err)
		stream, err := system.Stream(ctx, appBase+"-"+faker.Name(), faker.Name())
		require.NoError(t, err)

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
			ctx, done := context.WithCancel(context.Background())
			defer done()
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
