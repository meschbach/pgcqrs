package storage

import (
	"testing"
	"time"

	"github.com/go-faker/faker/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsumerStore_GetLock(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	domain := domainUniqueness.Next()
	stream := faker.Word()
	consumer := faker.Word()
	holder := faker.Word()

	t.Run("ReturnsLockState", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		_, err := store.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
		require.NoError(t, err)

		state, found, err := store.GetLock(ctx, domain, stream, consumer)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, state)
		assert.Equal(t, consumer, state.Consumer)
		assert.Equal(t, domain, state.Domain)
		assert.Equal(t, stream, state.Stream)
		assert.Equal(t, holder, state.Holder)
	})

	t.Run("ReturnsNilForNonExistent", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		state, found, err := store.GetLock(ctx, domain, stream, consumer)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Nil(t, state)
	})

	t.Run("ReturnsNilForExpired", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		consumerID, err := store.resolveConsumerName(ctx, consumer)
		require.NoError(t, err)

		streamID := resolveStreamID(ctx, t, pool, domain, stream)
		insertExpiredLock(ctx, t, pool, streamID, consumerID, holder)

		state, found, err := store.GetLock(ctx, domain, stream, consumer)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Nil(t, state)
	})
}

func TestConsumerStore_ListLocks(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	domain := domainUniqueness.Next()
	stream := faker.Word()

	t.Run("ReturnsActiveLocks", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		_, err := store.TryAcquire(ctx, domain, stream, "consumer-a", "holder-a", 30*time.Second)
		require.NoError(t, err)
		_, err = store.TryAcquire(ctx, domain, stream, "consumer-b", "holder-b", 30*time.Second)
		require.NoError(t, err)

		locks, err := store.ListLocks(ctx, domain, stream)
		require.NoError(t, err)
		require.Len(t, locks, 2)
	})

	t.Run("EmptyWhenNoLocks", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		locks, err := store.ListLocks(ctx, domain, stream)
		require.NoError(t, err)
		assert.Empty(t, locks)
	})

	t.Run("ExcludesExpiredLocks", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		// Insert an expired lock directly
		consumerID1, err := store.resolveConsumerName(ctx, "consumer-expired")
		require.NoError(t, err)
		streamID := resolveStreamID(ctx, t, pool, domain, stream)
		insertExpiredLock(ctx, t, pool, streamID, consumerID1, "holder-expired")

		// Acquire an active lock
		_, err = store.TryAcquire(ctx, domain, stream, "consumer-active", "holder-active", 30*time.Second)
		require.NoError(t, err)

		// ListLocks should only return the active lock
		locks, err := store.ListLocks(ctx, domain, stream)
		require.NoError(t, err)
		require.Len(t, locks, 1)
		assert.Equal(t, "consumer-active", locks[0].Consumer)
	})
}
