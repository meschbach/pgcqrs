package indexer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackoff_Configuration(t *testing.T) {
	t.Parallel()

	t.Run("DefaultConfiguration", func(t *testing.T) {
		t.Parallel()
		b := NewBackoff()
		require.NotNil(t, b)
		assert.Equal(t, 100*time.Millisecond, b.InitialInterval)
		assert.Equal(t, 30*time.Second, b.MaxInterval)
		assert.InEpsilon(t, 0.5, b.RandomizationFactor, 0.01)
		assert.InEpsilon(t, 2.0, b.Multiplier, 0.01)
	})

	t.Run("ResetClearsBackoff", func(t *testing.T) {
		t.Parallel()
		b := NewBackoff()

		_ = b.NextBackOff()
		_ = b.NextBackOff()

		b.Reset()
		d3 := b.NextBackOff()
		assert.LessOrEqual(t, d3, 200*time.Millisecond, "after reset, should return near initial interval")
	})
}
