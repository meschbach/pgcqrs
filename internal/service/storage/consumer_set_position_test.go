package storage

import (
	"testing"

	"github.com/go-faker/faker/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsumerStore_SetPosition_BackwardGuard(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	domain := domainUniqueness.Next()
	stream := faker.Word()
	consumer := faker.Word()

	t.Run("StreamNotFound", func(t *testing.T) {
		t.Parallel()
		store := NewConsumerStore(WithDatabaseConnection(t))

		_, err := store.SetPosition(ctx, domain, stream, consumer, 100)
		require.Error(t, err)
		var streamErr *StreamNotFoundError
		require.ErrorAs(t, err, &streamErr)
	})

	t.Run("BackwardPositionError", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		_, err := store.SetPosition(ctx, domain, stream, consumer, 100)
		require.NoError(t, err)

		_, err = store.SetPosition(ctx, domain, stream, consumer, 50)
		require.Error(t, err)
		var backwardErr *BackwardPositionError
		require.ErrorAs(t, err, &backwardErr)
		assert.Equal(t, int64(100), backwardErr.Current)
		assert.Equal(t, int64(50), backwardErr.Requested)
	})

	t.Run("ForwardAndIdempotentPosition", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		_, err := store.SetPosition(ctx, domain, stream, consumer, 100)
		require.NoError(t, err)

		result, err := store.SetPosition(ctx, domain, stream, consumer, 200)
		require.NoError(t, err)
		assert.Equal(t, int64(100), *result.PreviousEventID)
		assert.Equal(t, int64(200), result.CurrentEventID)

		result, err = store.SetPosition(ctx, domain, stream, consumer, 200)
		require.NoError(t, err)
		require.NotNil(t, result.PreviousEventID)
		assert.Equal(t, int64(200), *result.PreviousEventID)
		assert.Equal(t, int64(200), result.CurrentEventID)
	})
}
