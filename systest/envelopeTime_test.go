package systest

import (
	"github.com/go-faker/faker/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			retrieved, err := stream.EnvelopesFor(ctx, r.ID)
			require.NoError(t, err)
			assert.Equal(t, originalEnvelope[0].When, retrieved[0].When)
		})
	})
}
