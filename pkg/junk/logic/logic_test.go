package logic

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestLogicEngine(t *testing.T) {
	le := NewLogicEngine()

	t.Run("simple test", func(t *testing.T) {
		ctx, done := context.WithTimeout(context.Background(), 1*time.Millisecond)
		defer done()

		result, err := le.True(ctx, "true.")
		require.NoError(t, err)
		assert.True(t, result)
	})
}
