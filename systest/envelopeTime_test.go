package systest

import (
	"context"
	"github.com/bxcodec/faker/v3"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
	"time"
)

const TimeExampleKind = "time-example"

type TimeExample struct {
	Value string
}

// TestSystemClient exercise the system from the point of view of the client.  A service is expected to be up and
// running.
func TestSystemClient(t *testing.T) {
	t.Run("With v1 Client", func(t *testing.T) {
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

		value := faker.Name()
		r, err := stream.Submit(ctx, TimeExampleKind, TimeExample{Value: value})
		require.NoError(t, err)
		originalEnvelope, err := stream.EnvelopesFor(ctx, r.ID)
		require.NoError(t, err)
		time.Sleep(100 * time.Millisecond)

		t.Run("EnvelopesFor", func(t *testing.T) {
			retrieved, err := stream.EnvelopesFor(ctx, r.ID)
			require.NoError(t, err)
			assert.Equal(t, originalEnvelope[0].When, retrieved[0].When)
		})
	})
}
