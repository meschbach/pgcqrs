package storage

import (
	"context"
	"testing"

	"github.com/go-faker/faker/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPositionStore_SetPosition(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	domain := faker.Word()
	stream := faker.Word()
	consumer := faker.Word()

	t.Run("StreamNotFound", func(t *testing.T) {
		t.Parallel()
		store := NewPositionStore(WithDatabaseConnection(t))

		_, err := store.SetPosition(ctx, domain, stream, consumer, int64(100))
		require.Error(t, err)
		var streamErr *StreamNotFoundError
		require.ErrorAs(t, err, &streamErr)
	})

	t.Run("InsertNewPosition", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewPositionStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		eventID := int64(100)
		result, err := store.SetPosition(ctx, domain, stream, consumer, eventID)
		require.NoError(t, err)
		assert.Nil(t, result.PreviousEventID)
		assert.Equal(t, int64(100), result.CurrentEventID)
	})

	t.Run("UpdatePositionForward", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewPositionStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		eventID := int64(100)
		result, err := store.SetPosition(ctx, domain, stream, consumer, eventID)
		require.NoError(t, err)
		assert.Nil(t, result.PreviousEventID)
		assert.Equal(t, int64(100), result.CurrentEventID)

		result2, err := store.SetPosition(ctx, domain, stream, consumer, int64(150))
		require.NoError(t, err)
		assert.Equal(t, int64(100), *result2.PreviousEventID)
		assert.Equal(t, int64(150), result2.CurrentEventID)
	})

	t.Run("BackwardMoveError", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewPositionStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		_, err := store.SetPosition(ctx, domain, stream, consumer, int64(100))
		require.NoError(t, err)

		_, err = store.SetPosition(ctx, domain, stream, consumer, int64(50))
		assert.Error(t, err)
	})

	t.Run("SamePositionAllowed", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewPositionStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		_, err := store.SetPosition(ctx, domain, stream, consumer, int64(100))
		require.NoError(t, err)

		result2, err := store.SetPosition(ctx, domain, stream, consumer, int64(100))
		require.NoError(t, err)
		assert.Equal(t, int64(100), *result2.PreviousEventID)
		assert.Equal(t, int64(100), result2.CurrentEventID)
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewPositionStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		cancelCtx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := store.SetPosition(cancelCtx, domain, stream, consumer, int64(100))
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestPositionStore_GetPosition(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	domain := faker.Word()
	stream := faker.Word()
	consumer := faker.Word()

	t.Run("PositionExists", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewPositionStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		_, err := store.SetPosition(ctx, domain, stream, consumer, int64(456))
		require.NoError(t, err)
		pos, found, err := store.GetPosition(ctx, domain, stream, consumer)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, int64(456), pos)
	})

	t.Run("PositionNotFound", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewPositionStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		pos, found, err := store.GetPosition(ctx, domain, stream, consumer)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Zero(t, pos)
	})

	t.Run("NoStreamNoPosition", func(t *testing.T) {
		t.Parallel()
		store := NewPositionStore(WithDatabaseConnection(t))

		pos, found, err := store.GetPosition(ctx, domain, stream, consumer)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Zero(t, pos)
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewPositionStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		_, err := store.SetPosition(ctx, domain, stream, consumer, int64(100))
		require.NoError(t, err)

		cancelCtx, cancel := context.WithCancel(t.Context())
		cancel()

		_, _, err = store.GetPosition(cancelCtx, domain, stream, consumer)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestPositionStore_ListConsumers(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	domain := faker.Word()
	stream := faker.Word()

	t.Run("MultipleConsumers", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewPositionStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		consumers := []string{"consumer1", "consumer2", "consumer3"}
		for _, c := range consumers {
			_, err := store.SetPosition(ctx, domain, stream, c, int64(100))
			require.NoError(t, err)
		}

		result, err := store.ListConsumers(ctx, domain, stream)
		require.NoError(t, err)
		assert.ElementsMatch(t, consumers, result)
	})

	t.Run("NoConsumers", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewPositionStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		result, err := store.ListConsumers(ctx, domain, stream)
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("NoStreamNoConsumers", func(t *testing.T) {
		t.Parallel()
		store := NewPositionStore(WithDatabaseConnection(t))

		result, err := store.ListConsumers(ctx, domain, stream)
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewPositionStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		cancelCtx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := store.ListConsumers(cancelCtx, domain, stream)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestPositionStore_DeletePosition(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	domain := faker.Word()
	stream := faker.Word()
	consumer := faker.Word()

	t.Run("DeleteExisting", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewPositionStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		err := store.DeletePosition(ctx, domain, stream, consumer)
		require.NoError(t, err)
	})

	t.Run("DeleteNonExisting", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewPositionStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		err := store.DeletePosition(ctx, domain, stream, consumer)
		require.NoError(t, err)
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewPositionStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		cancelCtx, cancel := context.WithCancel(t.Context())
		cancel()

		err := store.DeletePosition(cancelCtx, domain, stream, consumer)
		assert.ErrorIs(t, err, context.Canceled)
	})
}
