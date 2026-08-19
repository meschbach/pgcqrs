package storage

import (
	"testing"
	"time"

	"github.com/go-faker/faker/v4"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsumerStore_HeartbeatWithPosition(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	domain := domainUniqueness.Next()
	stream := faker.Word()
	consumer := faker.Word()
	holder := faker.Word()

	t.Run("SuccessfulHeartbeat", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		_, _, err := store.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
		require.NoError(t, err)

		before, foundBefore, err := store.GetLock(ctx, domain, stream, consumer)
		require.NoError(t, err)
		require.True(t, foundBefore)
		require.NotNil(t, before)

		err = store.HeartbeatWithPosition(ctx, domain, stream, consumer, holder, 10)
		require.NoError(t, err)

		pos, found, err := store.GetPosition(ctx, domain, stream, consumer)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, int64(10), pos)

		after, foundAfter, err := store.GetLock(ctx, domain, stream, consumer)
		require.NoError(t, err)
		require.True(t, foundAfter)
		require.NotNil(t, after)
		assert.True(t, after.HeldUntil.After(before.HeldUntil), "held_until should be extended")
		assert.True(t, after.HeartbeatAt.After(before.HeartbeatAt), "heartbeat_at should be updated")
	})

	t.Run("StalePositionReturnsConflict", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		_, _, err := store.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
		require.NoError(t, err)

		err = store.HeartbeatWithPosition(ctx, domain, stream, consumer, holder, 100)
		require.NoError(t, err)

		err = store.HeartbeatWithPosition(ctx, domain, stream, consumer, holder, 50)
		require.Error(t, err)
		var conflict *v1.HeartbeatConflictError
		require.ErrorAs(t, err, &conflict)
		assert.Equal(t, int64(50), conflict.TargetVersion)
		assert.Equal(t, int64(100), conflict.CurrentVersion)
	})

	t.Run("LockNotAcquiredReturnsNotFoundError", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		err := store.HeartbeatWithPosition(ctx, domain, stream, consumer, holder, 10)
		require.Error(t, err)
		var lockNotFound *v1.LockNotFoundError
		require.ErrorAs(t, err, &lockNotFound)
	})

	t.Run("ExpiredLockReturnsLockExpiredError", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		consumerID, err := store.resolveConsumerName(ctx, consumer)
		require.NoError(t, err)

		streamID := resolveStreamID(ctx, t, pool, domain, stream)
		insertExpiredLock(ctx, t, pool, streamID, consumerID, holder)

		err = store.HeartbeatWithPosition(ctx, domain, stream, consumer, holder, 10)
		require.Error(t, err)
		var lockExpired *v1.LockExpiredError
		require.ErrorAs(t, err, &lockExpired)
	})

	t.Run("StolenLockReturnsLockNotHeldError", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		// Acquire lock with holder1
		_, _, err := store.TryAcquire(ctx, domain, stream, consumer, "holder1", 30*time.Second)
		require.NoError(t, err)

		// Try to heartbeat with holder2 (different from holder1)
		err = store.HeartbeatWithPosition(ctx, domain, stream, consumer, "holder2", 10)
		require.Error(t, err)
		var lockNotHeld *v1.LockNotHeldError
		require.ErrorAs(t, err, &lockNotHeld)
		assert.Equal(t, "holder2", lockNotHeld.Holder)
	})
}
