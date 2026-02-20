package systest

import (
	"testing"
	"time"

	"github.com/go-faker/faker/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const TimeExampleKind = "time-example"

type TimeExample struct {
	Value string
}

// TestSystemClient exercise the system from the point of view of the client.  A service is expected to be up and
// running.
func TestSystemClient(t *testing.T) {
	t.Parallel()
	t.Run("With v1 Client", func(t *testing.T) {
		t.Parallel()
		harness := setupHarness()
		ctx := harness.ctx
		t.Cleanup(func() {
			harness.done()
		})
		stream := harness.stream

		value := faker.Name()
		r, err := stream.Submit(ctx, TimeExampleKind, TimeExample{Value: value})
		require.NoError(t, err)
		originalEnvelope, err := stream.EnvelopesFor(ctx, r.ID)
		require.NoError(t, err)
		time.Sleep(100 * time.Millisecond)

		t.Run("EnvelopesFor", func(t *testing.T) {
			t.Parallel()
			retrieved, err := stream.EnvelopesFor(ctx, r.ID)
			require.NoError(t, err)
			if assert.Len(t, retrieved, 1, "required at a single result") {
				assert.Equal(t, originalEnvelope[0].When, retrieved[0].When)
			}
		})
	})
}
