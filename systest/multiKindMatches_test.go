package systest

import (
	"context"
	"encoding/json"
	"github.com/bxcodec/faker/v3"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

const ExampleKind1 = "example-kind"

type Example struct {
	Value string
}

// Tests the systems capability to use an `or` clause between two matches.
func TestMultiKindMatch(t *testing.T) {
	t.Run("With v1 Client matching multiple kinds", func(t *testing.T) {
		t.Skip("Need to rethink storage layer for this")

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

		value1 := faker.Name()
		value1Sub, err := stream.Submit(ctx, ExampleKind1, Example{Value: value1})
		require.NoError(t, err)

		value2 := faker.Name()
		value2Sub, err := stream.Submit(ctx, ExampleKind1, Example{Value: value2})
		require.NoError(t, err)

		t.Run("Able to match multiple of same kind", func(t *testing.T) {
			ctx, done := context.WithCancel(context.Background())
			defer done()
			q := stream.Query()
			found1 := false
			found2 := false
			q.WithKind(ExampleKind1).Match(Example{Value: value1}).On(func(ctx context.Context, e v1.Envelope, rawJSON json.RawMessage) error {
				found1 = true
				assert.Equal(t, value1Sub.ID, e.ID)
				return nil
			})
			q.WithKind(ExampleKind1).Match(Example{Value: value2}).On(func(ctx context.Context, e v1.Envelope, rawJSON json.RawMessage) error {
				found2 = true
				assert.Equal(t, value2Sub.ID, e.ID)
				return nil
			})
			require.NoError(t, q.Stream(ctx))
			assert.True(t, found1, "Found value1")
			assert.True(t, found2, "Found value2")
		})
	})
}
