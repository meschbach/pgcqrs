package v1

import (
	"context"
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

		lock, position, err := wire.WaitForLock(ctx, "domain", "stream", "consumer", "holder", 30*time.Second)
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

		lock1, _, err := wire.WaitForLock(ctx, domain, stream, consumer, holder1, 30*time.Second)
		require.NoError(t, err)
		require.NotNil(t, lock1)

		done := make(chan struct{})
		go func() {
			defer close(done)
			time.Sleep(50 * time.Millisecond)
			err := lock1.Release(ctx)
			assert.NoError(t, err)
		}()

		lock2, _, err := wire.WaitForLock(ctx, domain, stream, consumer, holder2, 30*time.Second)
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

		_, _, err = wire.WaitForLock(ctx, "domain", "stream", "consumer", "holder2", 30*time.Second)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("ReturnsPosition", func(t *testing.T) {
		t.Parallel()
		transport := NewMemoryTransport()
		wire := NewMemoryWire(transport)
		ctx := t.Context()

		_, err := transport.SetPosition(ctx, "domain", "stream", "consumer", 42)
		require.NoError(t, err)

		lock, position, err := wire.WaitForLock(ctx, "domain", "stream", "consumer", "holder", 30*time.Second)
		require.NoError(t, err)
		require.NotNil(t, lock)
		assert.Equal(t, int64(42), position)

		err = lock.Release(ctx)
		require.NoError(t, err)
	})
}

func TestMemoryLock_Heartbeat(t *testing.T) {
	t.Parallel()

	t.Run("HeartbeatUpdatesPosition", func(t *testing.T) {
		t.Parallel()
		transport := NewMemoryTransport()
		wire := NewMemoryWire(transport)
		ctx := t.Context()

		lock, _, err := wire.WaitForLock(ctx, "domain", "stream", "consumer", "holder", 30*time.Second)
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

		lock, _, err := wire.WaitForLock(ctx, "domain", "stream", "consumer", "holder", 30*time.Second)
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
