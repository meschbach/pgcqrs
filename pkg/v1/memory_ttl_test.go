package v1

import (
	"context"
	"testing"
	"time"

	"testing/synctest"

	"github.com/go-faker/faker/v4"
	"github.com/meschbach/pgcqrs/pkg/junk/faking"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryTransport_TTLExpiryActiveNotification(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		domain := faker.Word()
		stream := faker.Word()
		consumer := faker.Word()
		holder := faker.Word()

		transport := NewMemoryTransport()
		t.Cleanup(func() { require.NoError(t, transport.Close()) })
		ctx := t.Context()

		// Acquire lock with 6 second TTL (minimum allowed)
		result, err := transport.TryAcquire(ctx, domain, stream, consumer, holder, 6*time.Second)
		require.NoError(t, err)
		require.True(t, result.Acquired)

		// Subscribe to lock release events
		eventCh := make(chan LockReleasedEvent, 1)
		unsub := transport.OnLockRelease(func(_ context.Context, evt LockReleasedEvent) error {
			eventCh <- evt
			return nil
		})
		defer unsub()

		// Wait for 7 seconds (lock should expire at 6s)
		// synctest will automatically advance time when all goroutines are blocked
		time.Sleep(7 * time.Second)

		// Verify we received the lock release event
		select {
		case evt := <-eventCh:
			assert.Equal(t, domain, evt.Domain)
			assert.Equal(t, stream, evt.Stream)
			assert.Equal(t, consumer, evt.Consumer)
			assert.Equal(t, holder, evt.Holder)
		default:
			t.Fatal("expected lock release event after TTL expiry")
		}

		// Verify the lock is actually gone
		state, found, err := transport.GetLock(ctx, domain, stream, consumer)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Nil(t, state)
	})
}

func TestMemoryTransport_ExplicitReleaseNotification(t *testing.T) {
	t.Parallel()

	domain := faker.Word()
	stream := faker.Word()
	consumer := faker.Word()
	holder := faker.Word()

	transport := NewMemoryTransport()
	t.Cleanup(func() { require.NoError(t, transport.Close()) })
	ctx := t.Context()

	// Acquire lock
	result, err := transport.TryAcquire(ctx, domain, stream, consumer, holder, 30*time.Second)
	require.NoError(t, err)
	require.True(t, result.Acquired)

	// Subscribe to lock release events
	eventCh := make(chan LockReleasedEvent, 1)
	unsub := transport.OnLockRelease(func(_ context.Context, evt LockReleasedEvent) error {
		eventCh <- evt
		return nil
	})
	defer unsub()

	// Release the lock
	err = transport.Release(ctx, domain, stream, consumer, holder)
	require.NoError(t, err)

	// Verify we received the event immediately
	select {
	case evt := <-eventCh:
		assert.Equal(t, domain, evt.Domain)
		assert.Equal(t, stream, evt.Stream)
		assert.Equal(t, consumer, evt.Consumer)
		assert.Equal(t, holder, evt.Holder)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected lock release event after explicit release")
	}
}

func TestMemoryTransport_WaitForLockWakesOnTTLExpiry(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		domain := faker.Word()
		stream := faker.Word()
		consumer := faker.Word()
		holderNames := faking.NewUniqueKebab()
		holder1 := holderNames.Next()
		holder2 := holderNames.Next()

		transport := NewMemoryTransport()
		t.Cleanup(func() { require.NoError(t, transport.Close()) })
		wire := NewMemoryWire(transport)
		ctx := t.Context()

		// holder1 acquires lock with 6 second TTL
		lock1, _, _, err := wire.WaitForLock(ctx, domain, stream, consumer, holder1, 6*time.Second)
		require.NoError(t, err)
		require.NotNil(t, lock1)

		// holder2 tries to acquire - should block
		done := make(chan struct{})
		var lock2 *MemoryLock
		var lockErr error
		go func() {
			defer close(done)
			lock2, _, _, lockErr = wire.WaitForLock(ctx, domain, stream, consumer, holder2, 30*time.Second)
		}()

		// Wait for holder2 to be blocked
		time.Sleep(100 * time.Millisecond)

		// Don't release lock1 - let it expire via TTL
		// Advance time past TTL
		time.Sleep(7 * time.Second)

		// holder2 should now have acquired the lock
		<-done
		require.NoError(t, lockErr)
		require.NotNil(t, lock2)

		// Verify holder2 has the lock
		state, found, err := transport.GetLock(ctx, domain, stream, consumer)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, holder2, state.Holder)
	})
}
