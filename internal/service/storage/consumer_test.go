package storage

import (
	"fmt"
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
	consumer := faker.Word()

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
	domain := faker.Word()
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
		assert.Equal(t, holder, result.HeldBy)
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
}

func TestConsumerStore_TryAcquire_CleansExpiredLocks(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	domain := faker.Word()
	stream := faker.Word()

	pool := WithDatabaseConnection(t)
	store := NewConsumerStore(pool)
	createStreamForTest(ctx, t, pool, domain, stream)

	consumerID, err := store.resolveConsumerName(ctx, "expired-consumer")
	require.NoError(t, err)

	var streamID int64
	err = pool.QueryRow(ctx, `SELECT id FROM events_stream WHERE app = $1 AND stream = $2`, domain, stream).Scan(&streamID)
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		cID, err := store.resolveConsumerName(ctx, fmt.Sprintf("expired-consumer-%d", i))
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `
			INSERT INTO consumer_locks (stream_id, consumer_id, holder, ttl, guarantee_until, held_until)
			VALUES ($1, $2, $3, '30s', NOW() - '10s'::interval, NOW() - '1s'::interval)
			ON CONFLICT (stream_id, consumer_id) DO UPDATE
			SET holder = EXCLUDED.holder, held_until = EXCLUDED.held_until`,
			streamID, cID, fmt.Sprintf("holder-%d", i))
		require.NoError(t, err)
	}
	_ = consumerID

	result, err := store.TryAcquire(ctx, domain, stream, "new-consumer", "new-holder", 30*time.Second)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Acquired)

	var expiredCount int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM consumer_locks cl
		JOIN consumer_names cn ON cl.consumer_id = cn.id
		WHERE cl.stream_id = $1 AND cn.name LIKE 'expired-consumer%'`,
		streamID).Scan(&expiredCount)
	require.NoError(t, err)
	assert.Equal(t, 0, expiredCount)
}

func TestConsumerStore_TryAcquire_ConcurrentAcquisition(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	domain := faker.Word()
	stream := faker.Word()
	consumer := faker.Word()

	pool := WithDatabaseConnection(t)
	store := NewConsumerStore(pool)
	createStreamForTest(ctx, t, pool, domain, stream)

	const numGoroutines = 10
	results := make(chan *v1.LockResult, numGoroutines)
	errors := make(chan error, numGoroutines)
	start := make(chan struct{})

	for i := 0; i < numGoroutines; i++ {
		go func(holderID int) {
			<-start
			result, err := store.TryAcquire(ctx, domain, stream, consumer, fmt.Sprintf("holder-%d", holderID), 30*time.Second)
			if err != nil {
				errors <- err
				return
			}
			results <- result
		}(i)
	}

	close(start)

	acquired := 0
	conflicts := 0
	for i := 0; i < numGoroutines; i++ {
		select {
		case result := <-results:
			if result.Acquired {
				acquired++
			} else {
				conflicts++
			}
		case err := <-errors:
			t.Fatalf("unexpected error: %v", err)
		}
	}

	assert.Equal(t, 1, acquired, "exactly one goroutine should acquire the lock")
	assert.Equal(t, numGoroutines-1, conflicts, "all other goroutines should get conflicts")
}

func TestConsumerStore_Release(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	domain := faker.Word()
	stream := faker.Word()
	consumer := faker.Word()
	holder := faker.Word()

	t.Run("ExplicitRelease", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		_, err := store.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
		require.NoError(t, err)

		err = store.Release(ctx, domain, stream, consumer, holder)
		require.NoError(t, err)

		state, found, err := store.GetLock(ctx, domain, stream, consumer)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Nil(t, state)
	})

	t.Run("IdempotentRelease", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		err := store.Release(ctx, domain, stream, consumer, holder)
		require.NoError(t, err)
	})

	t.Run("NonHolderRejection", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		_, err := store.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
		require.NoError(t, err)

		otherHolder := faker.Word()
		err = store.Release(ctx, domain, stream, consumer, otherHolder)
		require.Error(t, err)
		var lockNotHeld *v1.LockNotHeldError
		require.ErrorAs(t, err, &lockNotHeld)
		assert.Equal(t, consumer, lockNotHeld.Consumer)
		assert.Equal(t, otherHolder, lockNotHeld.Holder)

		state, found, err := store.GetLock(ctx, domain, stream, consumer)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, state)
		assert.Equal(t, holder, state.Holder)
	})
}

func TestConsumerStore_GetLock(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	domain := faker.Word()
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

		var streamID int64
		err = pool.QueryRow(ctx, `SELECT id FROM events_stream WHERE app = $1 AND stream = $2`, domain, stream).Scan(&streamID)
		require.NoError(t, err)

		_, err = pool.Exec(ctx, `
			INSERT INTO consumer_locks (stream_id, consumer_id, holder, ttl, guarantee_until, held_until)
			VALUES ($1, $2, $3, '30s', NOW() - '10s'::interval, NOW() - '1s'::interval)
			ON CONFLICT (stream_id, consumer_id) DO UPDATE
			SET holder = EXCLUDED.holder, held_until = EXCLUDED.held_until`,
			streamID, consumerID, holder)
		require.NoError(t, err)

		state, found, err := store.GetLock(ctx, domain, stream, consumer)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Nil(t, state)
	})
}

func TestConsumerStore_ListLocks(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	domain := faker.Word()
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
		var streamID int64
		err = pool.QueryRow(ctx, `SELECT id FROM events_stream WHERE app = $1 AND stream = $2`, domain, stream).Scan(&streamID)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `
			INSERT INTO consumer_locks (stream_id, consumer_id, holder, ttl, guarantee_until, held_until)
			VALUES ($1, $2, $3, '30s', NOW() - '10s'::interval, NOW() - '1s'::interval)`,
			streamID, consumerID1, "holder-expired")
		require.NoError(t, err)

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

func TestConsumerStore_HeartbeatWithPosition(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	domain := faker.Word()
	stream := faker.Word()
	consumer := faker.Word()
	holder := faker.Word()

	t.Run("SuccessfulHeartbeat", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		_, err := store.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
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

		_, err := store.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
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

		var streamID int64
		err = pool.QueryRow(ctx, `SELECT id FROM events_stream WHERE app = $1 AND stream = $2`, domain, stream).Scan(&streamID)
		require.NoError(t, err)

		_, err = pool.Exec(ctx, `
			INSERT INTO consumer_locks (stream_id, consumer_id, holder, ttl, guarantee_until, held_until)
			VALUES ($1, $2, $3, '30s', NOW() - '10s'::interval, NOW() - '1s'::interval)
			ON CONFLICT (stream_id, consumer_id) DO UPDATE
			SET holder = EXCLUDED.holder, held_until = EXCLUDED.held_until`,
			streamID, consumerID, holder)
		require.NoError(t, err)

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
		_, err := store.TryAcquire(ctx, domain, stream, consumer, "holder1", 30*time.Second)
		require.NoError(t, err)

		// Try to heartbeat with holder2 (different from holder1)
		err = store.HeartbeatWithPosition(ctx, domain, stream, consumer, "holder2", 10)
		require.Error(t, err)
		var lockNotHeld *v1.LockNotHeldError
		require.ErrorAs(t, err, &lockNotHeld)
		assert.Equal(t, "holder2", lockNotHeld.Holder)
	})
}

func TestConsumerStore_SetPosition_BackwardGuard(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	domain := faker.Word()
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

func TestConsumerStore_PositionDualRead(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	domain := faker.Word()
	stream := faker.Word()

	t.Run("GetPositionReadsPreMigrationRow", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		var streamID int64
		err := pool.QueryRow(ctx, `SELECT id FROM events_stream WHERE app = $1 AND stream = $2`, domain, stream).Scan(&streamID)
		require.NoError(t, err)

		_, err = pool.Exec(ctx, `
			INSERT INTO consumer_positions (stream_id, consumer, event_id, updated_at)
			VALUES ($1, $2, 100, NOW())`, streamID, "legacy-consumer")
		require.NoError(t, err)

		pos, found, err := store.GetPosition(ctx, domain, stream, "legacy-consumer")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, int64(100), pos)
	})

	t.Run("GetPositionReadsPostMigrationRow", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		_, err := store.SetPosition(ctx, domain, stream, "modern-consumer", 200)
		require.NoError(t, err)

		pos, found, err := store.GetPosition(ctx, domain, stream, "modern-consumer")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, int64(200), pos)
	})

	t.Run("GetPositionReturnsNotFoundForUnknownConsumer", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		pos, found, err := store.GetPosition(ctx, domain, stream, "nonexistent")
		require.NoError(t, err)
		assert.False(t, found)
		assert.Equal(t, int64(0), pos)
	})

	t.Run("ListConsumersMergesBothPaths", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		var streamID int64
		err := pool.QueryRow(ctx, `SELECT id FROM events_stream WHERE app = $1 AND stream = $2`, domain, stream).Scan(&streamID)
		require.NoError(t, err)

		_, err = pool.Exec(ctx, `
			INSERT INTO consumer_positions (stream_id, consumer, event_id, updated_at)
			VALUES ($1, $2, 10, NOW())`, streamID, "legacy-only")
		require.NoError(t, err)

		_, err = store.SetPosition(ctx, domain, stream, "modern-only", 20)
		require.NoError(t, err)

		consumers, err := store.ListConsumers(ctx, domain, stream)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"legacy-only", "modern-only"}, consumers)
	})

	t.Run("DeletePositionWorksWithPreMigrationRow", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		var streamID int64
		err := pool.QueryRow(ctx, `SELECT id FROM events_stream WHERE app = $1 AND stream = $2`, domain, stream).Scan(&streamID)
		require.NoError(t, err)

		_, err = pool.Exec(ctx, `
			INSERT INTO consumer_positions (stream_id, consumer, event_id, updated_at)
			VALUES ($1, $2, 10, NOW())`, streamID, "to-delete")
		require.NoError(t, err)

		err = store.DeletePosition(ctx, domain, stream, "to-delete")
		require.NoError(t, err)

		_, found, err := store.GetPosition(ctx, domain, stream, "to-delete")
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("DeletePositionWorksWithPostMigrationRow", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		_, err := store.SetPosition(ctx, domain, stream, "to-delete-modern", 10)
		require.NoError(t, err)

		err = store.DeletePosition(ctx, domain, stream, "to-delete-modern")
		require.NoError(t, err)

		_, found, err := store.GetPosition(ctx, domain, stream, "to-delete-modern")
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("SetPositionPromotesLegacyRowToDualWrite", func(t *testing.T) {
		t.Parallel()
		pool := WithDatabaseConnection(t)
		store := NewConsumerStore(pool)
		createStreamForTest(ctx, t, pool, domain, stream)

		var streamID int64
		err := pool.QueryRow(ctx, `SELECT id FROM events_stream WHERE app = $1 AND stream = $2`, domain, stream).Scan(&streamID)
		require.NoError(t, err)

		_, err = pool.Exec(ctx, `
			INSERT INTO consumer_positions (stream_id, consumer, event_id, updated_at)
			VALUES ($1, $2, 10, NOW())`, streamID, "promoted")
		require.NoError(t, err)

		var consumerIDBefore *int64
		err = pool.QueryRow(ctx, `
			SELECT consumer_id FROM consumer_positions
			WHERE stream_id = $1 AND consumer = $2`, streamID, "promoted").Scan(&consumerIDBefore)
		require.NoError(t, err)
		assert.Nil(t, consumerIDBefore, "pre-migration row should have NULL consumer_id")

		_, err = store.SetPosition(ctx, domain, stream, "promoted", 20)
		require.NoError(t, err)

		var consumerIDAfter *int64
		err = pool.QueryRow(ctx, `
			SELECT consumer_id FROM consumer_positions
			WHERE stream_id = $1 AND consumer = $2`, streamID, "promoted").Scan(&consumerIDAfter)
		require.NoError(t, err)
		assert.NotNil(t, consumerIDAfter, "after SetPosition, consumer_id should be populated")
	})
}
