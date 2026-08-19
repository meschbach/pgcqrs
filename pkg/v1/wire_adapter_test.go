package v1

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-faker/faker/v4"
	"github.com/meschbach/pgcqrs/pkg/junk/faking"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryWire_WaitForLock(t *testing.T) {
	t.Parallel()

	t.Run("AcquiresLockImmediately", func(t *testing.T) {
		t.Parallel()
		transport := NewMemoryTransport()
		wire := NewMemoryWire(transport)
		ctx := t.Context()

		lock, position, _, err := wire.WaitForLock(ctx, "domain", "stream", "consumer", "holder", 30*time.Second)
		require.NoError(t, err)
		require.NotNil(t, lock)
		assert.Equal(t, int64(0), position)

		err = lock.Release(ctx)
		require.NoError(t, err)
	})

	// WaitsForLockRelease verifies that a waiting consumer acquires the lock
	// once the current holder releases it. holder1 takes the lock first; a
	// second WaitForLock from holder2 blocks until lock1 is released, at which
	// point it is handed the lock.
	t.Run("WaitsForLockRelease", func(t *testing.T) {
		t.Parallel()
		transport := NewMemoryTransport()
		wire := NewMemoryWire(transport)
		ctx := t.Context()

		domain := faker.Word()
		stream := faker.Word()
		consumer := faker.Word()
		holderNames := faking.NewUniqueKebab()
		holder1 := holderNames.Next()
		holder2 := holderNames.Next()

		lock1, _, _, err := wire.WaitForLock(ctx, domain, stream, consumer, holder1, 30*time.Second)
		require.NoError(t, err)
		require.NotNil(t, lock1)

		done := make(chan struct{})
		go func() {
			defer close(done)
			time.Sleep(50 * time.Millisecond)
			err := lock1.Release(ctx)
			assert.NoError(t, err)
		}()

		lock2, _, _, err := wire.WaitForLock(ctx, domain, stream, consumer, holder2, 30*time.Second)
		require.NoError(t, err)
		require.NotNil(t, lock2)

		<-done

		err = lock2.Release(ctx)
		require.NoError(t, err)
	})

	t.Run("RespectsContextCancellation", func(t *testing.T) {
		t.Parallel()
		transport := NewMemoryTransport()
		wire := NewMemoryWire(transport)

		result, err := transport.TryAcquire(t.Context(), "domain", "stream", "consumer", "holder1", 30*time.Second)
		require.NoError(t, err)
		require.True(t, result.Acquired)

		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()

		_, _, _, err = wire.WaitForLock(ctx, "domain", "stream", "consumer", "holder2", 30*time.Second)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("ReturnsPosition", func(t *testing.T) {
		t.Parallel()
		transport := NewMemoryTransport()
		wire := NewMemoryWire(transport)
		ctx := t.Context()

		_, err := transport.SetPosition(ctx, "domain", "stream", "consumer", 42)
		require.NoError(t, err)

		lock, position, _, err := wire.WaitForLock(ctx, "domain", "stream", "consumer", "holder", 30*time.Second)
		require.NoError(t, err)
		require.NotNil(t, lock)
		assert.Equal(t, int64(42), position)

		err = lock.Release(ctx)
		require.NoError(t, err)
	})
}

// TestMemoryWire_WaitForLock_LostWakeupRace verifies that a release event
// firing between a failed acquire and the select is still delivered. The
// old code (tryAcquire → subscribe → block) missed releases in this window;
// the new code (subscribe → tryAcquire → block) catches them.
func TestMemoryWire_WaitForLock_LostWakeupRace(t *testing.T) {
	t.Parallel()

	t.Run("ReleaseDuringAcquireWindowIsDelivered", func(t *testing.T) {
		t.Parallel()
		transport := NewMemoryTransport()
		wire := NewMemoryWire(transport)
		ctx := t.Context()

		domain := faker.Word()
		stream := faker.Word()
		consumer := faker.Word()
		require.NoError(t, transport.EnsureStream(ctx, domain, stream))

		holderNames := faking.NewUniqueKebab()
		holder1 := holderNames.Next()
		holder2 := holderNames.Next()

		// Holder1 takes the lock
		lock1, _, _, err := wire.WaitForLock(ctx, domain, stream, consumer, holder1, 30*time.Second)
		require.NoError(t, err)
		require.NotNil(t, lock1)

		// Release in a goroutine after a short delay. This fires while
		// holder2 is blocked inside tryAcquire (before the select).
		ready := make(chan struct{})
		releaseDone := make(chan error, 1)
		go func() {
			close(ready)
			time.Sleep(5 * time.Millisecond)
			releaseDone <- lock1.Release(ctx)
		}()

		<-ready

		// With the old code (subscribe after tryAcquire), this blocks
		// forever because the release event was missed. With the new
		// code (subscribe before tryAcquire), the event is buffered
		// and delivered.
		lock2, _, _, err := wire.WaitForLock(ctx, domain, stream, consumer, holder2, 30*time.Second)
		require.NoError(t, err)
		require.NotNil(t, lock2)
		require.NoError(t, lock2.Release(ctx))
		require.NoError(t, <-releaseDone)
	})

	t.Run("MultipleReleasesAreDelivered", func(t *testing.T) {
		t.Parallel()
		transport := NewMemoryTransport()
		wire := NewMemoryWire(transport)
		ctx := t.Context()

		domain := faker.Word()
		stream := faker.Word()
		consumer := faker.Word()
		require.NoError(t, transport.EnsureStream(ctx, domain, stream))

		// Three holders take and release in sequence
		for i := 0; i < 3; i++ {
			holder := fmt.Sprintf("holder-%d", i)
			lock, _, _, err := wire.WaitForLock(ctx, domain, stream, consumer, holder, 30*time.Second)
			require.NoError(t, err)
			require.NotNil(t, lock)
			require.NoError(t, lock.Release(ctx))
		}
	})

	t.Run("ConcurrentWaitersBothAcquire", func(t *testing.T) {
		t.Parallel()
		transport := NewMemoryTransport()
		wire := NewMemoryWire(transport)
		ctx := t.Context()

		domain := faker.Word()
		stream := faker.Word()
		consumer := faker.Word()
		require.NoError(t, transport.EnsureStream(ctx, domain, stream))

		holderNames := faking.NewUniqueKebab()
		holder1 := holderNames.Next()
		holder2 := holderNames.Next()
		holder3 := holderNames.Next()

		// Holder1 takes the lock
		lock1, _, _, err := wire.WaitForLock(ctx, domain, stream, consumer, holder1, 30*time.Second)
		require.NoError(t, err)

		type result struct {
			lock *MemoryLock
			err  error
		}
		ch := make(chan result, 2)

		// Two waiters start concurrently
		go func() {
			lock, _, _, err := wire.WaitForLock(ctx, domain, stream, consumer, holder2, 30*time.Second)
			ch <- result{lock, err}
		}()
		go func() {
			lock, _, _, err := wire.WaitForLock(ctx, domain, stream, consumer, holder3, 30*time.Second)
			ch <- result{lock, err}
		}()

		// Release so both can proceed one at a time
		time.Sleep(10 * time.Millisecond)
		require.NoError(t, lock1.Release(ctx))

		// First waiter gets the lock
		r1 := <-ch
		require.NoError(t, r1.err)
		require.NotNil(t, r1.lock)

		// Release so the second waiter can get it
		require.NoError(t, r1.lock.Release(ctx))

		r2 := <-ch
		require.NoError(t, r2.err)
		require.NotNil(t, r2.lock)
		require.NoError(t, r2.lock.Release(ctx))
	})
}

func TestMemoryLock_Heartbeat(t *testing.T) {
	t.Parallel()

	t.Run("HeartbeatUpdatesPosition", func(t *testing.T) {
		t.Parallel()
		transport := NewMemoryTransport()
		wire := NewMemoryWire(transport)
		ctx := t.Context()

		lock, _, _, err := wire.WaitForLock(ctx, "domain", "stream", "consumer", "holder", 30*time.Second)
		require.NoError(t, err)
		require.NotNil(t, lock)

		err = lock.Heartbeat(ctx, 100)
		require.NoError(t, err)

		position, found, err := transport.GetPosition(ctx, "domain", "stream", "consumer")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, int64(100), position)

		err = lock.Release(ctx)
		require.NoError(t, err)
	})
}

func TestMemoryLock_Release(t *testing.T) {
	t.Parallel()

	t.Run("ReleaseRemovesLock", func(t *testing.T) {
		t.Parallel()
		transport := NewMemoryTransport()
		wire := NewMemoryWire(transport)
		ctx := t.Context()

		lock, _, _, err := wire.WaitForLock(ctx, "domain", "stream", "consumer", "holder", 30*time.Second)
		require.NoError(t, err)
		require.NotNil(t, lock)

		err = lock.Release(ctx)
		require.NoError(t, err)

		state, found, err := transport.GetLock(ctx, "domain", "stream", "consumer")
		require.NoError(t, err)
		assert.False(t, found)
		assert.Nil(t, state)
	})
}
