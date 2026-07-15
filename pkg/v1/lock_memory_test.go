package v1

import (
	"fmt"
	"testing"
	"time"

	"github.com/go-faker/faker/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestMemory(t *testing.T) *memory {
	t.Helper()
	mem := NewMemoryTransport()
	m, ok := mem.(*memory)
	require.True(t, ok)
	return m
}

func TestMemoryTryAcquire(t *testing.T) {
	t.Parallel()

	domain := faker.Word()
	stream := faker.Word()
	consumer := faker.Word()
	holder := faker.Word()

	t.Run("RejectsTTLBelowMinimum", func(t *testing.T) {
		t.Parallel()
		m := newTestMemory(t)
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		_, err := m.TryAcquire(ctx, domain, stream, consumer, holder, 5*time.Second)
		require.Error(t, err)
		var ttlErr *TTLTooLowError
		require.ErrorAs(t, err, &ttlErr)
		assert.Equal(t, 5*time.Second, ttlErr.Provided)
		assert.Equal(t, LockMinimumTTL, ttlErr.Minimum)
	})

	t.Run("AcquireNewLock", func(t *testing.T) {
		t.Parallel()
		m := newTestMemory(t)
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		result, err := m.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Acquired)
		assert.Equal(t, holder, result.HeldBy)
	})

	t.Run("ConflictingLockReturnsHeldBy", func(t *testing.T) {
		t.Parallel()
		m := newTestMemory(t)
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		_, err := m.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
		require.NoError(t, err)

		otherHolder := faker.Word()
		result, err := m.TryAcquire(ctx, domain, stream, consumer, otherHolder, 30*time.Second)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.Acquired)
		assert.Equal(t, holder, result.HeldBy)
	})

	t.Run("SameHolderReacquires", func(t *testing.T) {
		t.Parallel()
		m := newTestMemory(t)
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		result1, err := m.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
		require.NoError(t, err)
		require.True(t, result1.Acquired)

		result2, err := m.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
		require.NoError(t, err)
		require.NotNil(t, result2)
		assert.True(t, result2.Acquired)
	})

	t.Run("CleansExpiredLocks", func(t *testing.T) {
		t.Parallel()
		frozen := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		m := newTestMemory(t)
		m.now = func() time.Time { return frozen }
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		for i := 0; i < 5; i++ {
			_, err := m.TryAcquire(ctx, domain, stream, fmt.Sprintf("consumer-%d", i), fmt.Sprintf("holder-%d", i), 10*time.Second)
			require.NoError(t, err)
		}

		m.now = func() time.Time { return frozen.Add(11 * time.Second) }

		result, err := m.TryAcquire(ctx, domain, stream, "new-consumer", "new-holder", 10*time.Second)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Acquired)

		locks, err := m.ListLocks(ctx, domain, stream)
		require.NoError(t, err)
		assert.Len(t, locks, 1)
		assert.Equal(t, "new-consumer", locks[0].Consumer)
	})

	t.Run("ConcurrentAcquisition", func(t *testing.T) {
		t.Parallel()
		m := newTestMemory(t)
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		const numGoroutines = 10
		results := make(chan *LockResult, numGoroutines)
		errors := make(chan error, numGoroutines)
		start := make(chan struct{})

		for i := 0; i < numGoroutines; i++ {
			go func(holderID int) {
				<-start
				result, err := m.TryAcquire(ctx, domain, stream, consumer, fmt.Sprintf("holder-%d", holderID), 30*time.Second)
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
	})
}

func TestMemoryRelease(t *testing.T) {
	t.Parallel()

	domain := faker.Word()
	stream := faker.Word()
	consumer := faker.Word()
	holder := faker.Word()

	t.Run("ExplicitRelease", func(t *testing.T) {
		t.Parallel()
		m := newTestMemory(t)
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		_, err := m.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
		require.NoError(t, err)

		err = m.Release(ctx, domain, stream, consumer, holder)
		require.NoError(t, err)

		state, found, err := m.GetLock(ctx, domain, stream, consumer)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Nil(t, state)
	})

	t.Run("IdempotentRelease", func(t *testing.T) {
		t.Parallel()
		m := newTestMemory(t)
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		err := m.Release(ctx, domain, stream, consumer, holder)
		require.NoError(t, err)
	})

	t.Run("NonHolderReleaseReturnsError", func(t *testing.T) {
		t.Parallel()
		m := newTestMemory(t)
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		_, err := m.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
		require.NoError(t, err)

		otherHolder := faker.Word()
		err = m.Release(ctx, domain, stream, consumer, otherHolder)
		require.Error(t, err)
		var lockNotHeld *LockNotHeldError
		require.ErrorAs(t, err, &lockNotHeld)
		assert.Equal(t, consumer, lockNotHeld.Consumer)
		assert.Equal(t, otherHolder, lockNotHeld.Holder)

		state, found, err := m.GetLock(ctx, domain, stream, consumer)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, state)
		assert.Equal(t, holder, state.Holder)
	})
}

func TestMemoryGetLock(t *testing.T) {
	t.Parallel()

	domain := faker.Word()
	stream := faker.Word()
	consumer := faker.Word()
	holder := faker.Word()

	t.Run("ReturnsLockState", func(t *testing.T) {
		t.Parallel()
		m := newTestMemory(t)
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		_, err := m.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
		require.NoError(t, err)

		state, found, err := m.GetLock(ctx, domain, stream, consumer)
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
		m := newTestMemory(t)
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		state, found, err := m.GetLock(ctx, domain, stream, consumer)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Nil(t, state)
	})
}

func TestMemoryListLocks(t *testing.T) {
	t.Parallel()

	domain := faker.Word()
	stream := faker.Word()

	t.Run("ReturnsActiveLocks", func(t *testing.T) {
		t.Parallel()
		m := newTestMemory(t)
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		_, err := m.TryAcquire(ctx, domain, stream, "consumer-a", "holder-a", 30*time.Second)
		require.NoError(t, err)
		_, err = m.TryAcquire(ctx, domain, stream, "consumer-b", "holder-b", 30*time.Second)
		require.NoError(t, err)

		locks, err := m.ListLocks(ctx, domain, stream)
		require.NoError(t, err)
		assert.Len(t, locks, 2)
	})

	t.Run("EmptyWhenNoLocks", func(t *testing.T) {
		t.Parallel()
		m := newTestMemory(t)
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		locks, err := m.ListLocks(ctx, domain, stream)
		require.NoError(t, err)
		assert.Empty(t, locks)
	})

	t.Run("ExcludesExpiredLocks", func(t *testing.T) {
		t.Parallel()
		frozen := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		m := newTestMemory(t)
		m.now = func() time.Time { return frozen }
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		// Acquire a lock that will expire
		_, err := m.TryAcquire(ctx, domain, stream, "consumer-expired", "holder-expired", 10*time.Second)
		require.NoError(t, err)

		// Acquire an active lock with longer TTL
		_, err = m.TryAcquire(ctx, domain, stream, "consumer-active", "holder-active", 30*time.Second)
		require.NoError(t, err)

		// Advance time past the first lock's TTL but before the second expires
		m.now = func() time.Time { return frozen.Add(15 * time.Second) }

		// ListLocks should only return the active lock
		locks, err := m.ListLocks(ctx, domain, stream)
		require.NoError(t, err)
		require.Len(t, locks, 1)
		assert.Equal(t, "consumer-active", locks[0].Consumer)
	})
}

func TestMemoryHeartbeatWithPosition(t *testing.T) {
	t.Parallel()

	domain := faker.Word()
	stream := faker.Word()
	consumer := faker.Word()
	holder := faker.Word()

	t.Run("SuccessfulHeartbeat", func(t *testing.T) {
		t.Parallel()
		m := newTestMemory(t)
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		_, err := m.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
		require.NoError(t, err)

		before, foundBefore, err := m.GetLock(ctx, domain, stream, consumer)
		require.NoError(t, err)
		require.True(t, foundBefore)
		require.NotNil(t, before)

		err = m.HeartbeatWithPosition(ctx, domain, stream, consumer, holder, 10)
		require.NoError(t, err)

		pos, found, err := m.GetPosition(ctx, domain, stream, consumer)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, int64(10), pos)

		after, foundAfter, err := m.GetLock(ctx, domain, stream, consumer)
		require.NoError(t, err)
		require.True(t, foundAfter)
		require.NotNil(t, after)
		assert.True(t, after.HeldUntil.After(before.HeldUntil), "held_until should be extended")
		assert.True(t, after.HeartbeatAt.After(before.HeartbeatAt), "heartbeat_at should be updated")
	})

	t.Run("StalePositionNoUpdate", func(t *testing.T) {
		t.Parallel()
		m := newTestMemory(t)
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		_, err := m.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
		require.NoError(t, err)

		err = m.HeartbeatWithPosition(ctx, domain, stream, consumer, holder, 100)
		require.NoError(t, err)

		err = m.HeartbeatWithPosition(ctx, domain, stream, consumer, holder, 50)
		var conflictErr *HeartbeatConflictError
		require.ErrorAs(t, err, &conflictErr)
		assert.Equal(t, int64(50), conflictErr.TargetVersion)
		assert.Equal(t, int64(100), conflictErr.CurrentVersion)

		pos, found, err := m.GetPosition(ctx, domain, stream, consumer)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, int64(100), pos)
	})

	t.Run("ExpiredLockNoUpdate", func(t *testing.T) {
		t.Parallel()
		m := newTestMemory(t)
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		err := m.HeartbeatWithPosition(ctx, domain, stream, consumer, holder, 10)
		var expiredErr *LockExpiredError
		require.ErrorAs(t, err, &expiredErr)

		pos, found, err := m.GetPosition(ctx, domain, stream, consumer)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Equal(t, int64(0), pos)
	})

	t.Run("StolenLockReturnsLockNotHeldError", func(t *testing.T) {
		t.Parallel()
		m := newTestMemory(t)
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		// Acquire lock with holder1
		_, err := m.TryAcquire(ctx, domain, stream, consumer, "holder1", 30*time.Second)
		require.NoError(t, err)

		// Try to heartbeat with holder2 (different from holder1)
		err = m.HeartbeatWithPosition(ctx, domain, stream, consumer, "holder2", 10)
		require.Error(t, err)
		var lockNotHeld *LockNotHeldError
		require.ErrorAs(t, err, &lockNotHeld)
		assert.Equal(t, "holder2", lockNotHeld.Holder)
	})
}

func TestMemoryLockOptionOnSubmit(t *testing.T) {
	t.Parallel()

	domain := faker.Word()
	stream := faker.Word()
	consumer := faker.Word()
	holder := faker.Word()

	t.Run("ValidLockSucceeds", func(t *testing.T) {
		t.Parallel()
		m := newTestMemory(t)
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		_, err := m.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
		require.NoError(t, err)

		lock := NewLock(consumer, holder)
		result, err := m.Submit(ctx, domain, stream, "test-kind", map[string]string{"v": "1"}, lock)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, int64(0), result.ID)
	})

	t.Run("ExpiredLockRejected", func(t *testing.T) {
		t.Parallel()
		m := newTestMemory(t)
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		lock := NewLock(consumer, holder)
		_, err := m.Submit(ctx, domain, stream, "test-kind", map[string]string{"v": "1"}, lock)
		require.Error(t, err)
		var lockErr *LockNotHeldError
		require.ErrorAs(t, err, &lockErr)
	})

	t.Run("WrongHolderRejected", func(t *testing.T) {
		t.Parallel()
		m := newTestMemory(t)
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		_, err := m.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
		require.NoError(t, err)

		wrongLock := NewLock(consumer, "wrong-holder")
		_, err = m.Submit(ctx, domain, stream, "test-kind", map[string]string{"v": "1"}, wrongLock)
		require.Error(t, err)
		var lockErr *LockNotHeldError
		require.ErrorAs(t, err, &lockErr)
	})

	t.Run("NoLockBackwardCompatible", func(t *testing.T) {
		t.Parallel()
		m := newTestMemory(t)
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		result, err := m.Submit(ctx, domain, stream, "test-kind", map[string]string{"v": "1"})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, int64(0), result.ID)
	})
}

func TestMemoryClockInjection(t *testing.T) {
	t.Parallel()

	domain := faker.Word()
	stream := faker.Word()
	consumer := faker.Word()
	holder := faker.Word()

	t.Run("AdvancePastTTLTryAcquireSucceeds", func(t *testing.T) {
		t.Parallel()
		frozen := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		m := newTestMemory(t)
		m.now = func() time.Time { return frozen }
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		_, err := m.TryAcquire(ctx, domain, stream, consumer, holder, 10*time.Second)
		require.NoError(t, err)

		m.now = func() time.Time { return frozen.Add(11 * time.Second) }

		otherHolder := faker.Word()
		result, err := m.TryAcquire(ctx, domain, stream, consumer, otherHolder, 10*time.Second)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Acquired)
		assert.Equal(t, otherHolder, result.HeldBy)
	})

	t.Run("AdvancePastTTLGetLockReturnsNil", func(t *testing.T) {
		t.Parallel()
		frozen := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		m := newTestMemory(t)
		m.now = func() time.Time { return frozen }
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		_, err := m.TryAcquire(ctx, domain, stream, consumer, holder, 10*time.Second)
		require.NoError(t, err)

		m.now = func() time.Time { return frozen.Add(11 * time.Second) }

		state, found, err := m.GetLock(ctx, domain, stream, consumer)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Nil(t, state)
	})

	t.Run("AdvancePastTTLHeartbeatNoEffect", func(t *testing.T) {
		t.Parallel()
		frozen := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		m := newTestMemory(t)
		m.now = func() time.Time { return frozen }
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		_, err := m.TryAcquire(ctx, domain, stream, consumer, holder, 10*time.Second)
		require.NoError(t, err)

		m.now = func() time.Time { return frozen.Add(11 * time.Second) }

		err = m.HeartbeatWithPosition(ctx, domain, stream, consumer, holder, 10)
		var expiredErr *LockExpiredError
		require.ErrorAs(t, err, &expiredErr)

		pos, found, err := m.GetPosition(ctx, domain, stream, consumer)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Equal(t, int64(0), pos)
	})

	t.Run("AdvancePastTTLSubmitLockRejected", func(t *testing.T) {
		t.Parallel()
		frozen := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		m := newTestMemory(t)
		m.now = func() time.Time { return frozen }
		ctx := t.Context()
		require.NoError(t, m.EnsureStream(ctx, domain, stream))

		_, err := m.TryAcquire(ctx, domain, stream, consumer, holder, 10*time.Second)
		require.NoError(t, err)

		m.now = func() time.Time { return frozen.Add(11 * time.Second) }

		lock := NewLock(consumer, holder)
		_, err = m.Submit(ctx, domain, stream, "test-kind", map[string]string{"v": "1"}, lock)
		require.Error(t, err)
		var lockErr *LockNotHeldError
		require.ErrorAs(t, err, &lockErr)
	})
}
