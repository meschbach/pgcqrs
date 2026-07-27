package storage

import (
	"testing"
	"time"

	"github.com/go-faker/faker/v4"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsumerStore_ResolveConsumerName(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	consumer := domainUniqueness.Next()

	t.Run("SameNameReturnsSameID", func(t *testing.T) {
		t.Parallel()
		store := NewConsumerStore(WithDatabaseConnection(t))

		id1, err := store.resolveConsumerName(ctx, consumer)
		require.NoError(t, err)
		require.NotZero(t, id1)

		id2, err := store.resolveConsumerName(ctx, consumer)
		require.NoError(t, err)
		assert.Equal(t, id1, id2)
	})

	t.Run("DifferentNamesReturnDifferentIDs", func(t *testing.T) {
		t.Parallel()
		store := NewConsumerStore(WithDatabaseConnection(t))

		id1, err := store.resolveConsumerName(ctx, faker.Word())
		require.NoError(t, err)

		id2, err := store.resolveConsumerName(ctx, faker.Word())
		require.NoError(t, err)
		assert.NotEqual(t, id1, id2)
	})
}

func TestConsumerStore_TryAcquire(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	domain := domainUniqueness.Next()
	stream := faker.Word()
	consumer := faker.Word()
	holder := faker.Word()

	t.Run("RejectsTTLBelowMinimum", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		_, err := store.TryAcquire(ctx, domain, stream, consumer, holder, 5*time.Second)
		require.Error(t, err)
		var ttlErr *v1.TTLTooLowError
		require.ErrorAs(t, err, &ttlErr)
		assert.Equal(t, 5*time.Second, ttlErr.Provided)
		assert.Equal(t, v1.LockMinimumTTL, ttlErr.Minimum)
	})

	t.Run("AcquireNewLock", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		result, err := store.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Acquired)
		assert.Equal(t, holder, result.HeldBy)
	})

	t.Run("ConflictingLockReturnsHeldBy", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		_, err := store.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
		require.NoError(t, err)

		otherHolder := faker.Word()
		result, err := store.TryAcquire(ctx, domain, stream, consumer, otherHolder, 30*time.Second)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.Acquired)
		assert.Equal(t, holder, result.HeldBy,
			"HeldBy must contain the previous holder's name, not the caller's")
	})

	t.Run("StreamNotFound", func(t *testing.T) {
		t.Parallel()
		store := NewConsumerStore(WithDatabaseConnection(t))

		_, err := store.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
		require.Error(t, err)
		var streamErr *StreamNotFoundError
		require.ErrorAs(t, err, &streamErr)
	})

	t.Run("SameHolderReacquires", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		result1, err := store.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
		require.NoError(t, err)
		require.True(t, result1.Acquired)

		result2, err := store.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
		require.NoError(t, err)
		require.NotNil(t, result2)
		assert.True(t, result2.Acquired)
		assert.Equal(t, holder, result2.HeldBy)
	})

	t.Run("ExpiredLockOverwritable", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		consumerID, err := store.resolveConsumerName(ctx, consumer)
		require.NoError(t, err)
		streamID := resolveStreamID(ctx, t, pool, domain, stream)
		insertExpiredLock(ctx, t, pool, streamID, consumerID, "holder-expired")

		result, err := store.TryAcquire(ctx, domain, stream, consumer, "holder-new", 30*time.Second)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Acquired,
			"new holder should successfully acquire over an expired lock")
		assert.Equal(t, "holder-new", result.HeldBy)
	})
}
