package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBus_LockReleasedEvent(t *testing.T) {
	t.Parallel()

	t.Run("DispatchOnLockReleased", func(t *testing.T) {
		t.Parallel()
		b := newBus()
		ctx := t.Context()

		eventCh := make(chan LockReleasedEvent, 1)
		sub := b.onLockRelease.OnE(func(_ context.Context, evt LockReleasedEvent) error {
			eventCh <- evt
			return nil
		})
		defer sub.Off()

		b.dispatchOnLockReleased(ctx, "domain", "stream", "consumer", "holder")

		select {
		case evt := <-eventCh:
			assert.Equal(t, "domain", evt.Domain)
			assert.Equal(t, "stream", evt.Stream)
			assert.Equal(t, "consumer", evt.Consumer)
			assert.Equal(t, "holder", evt.Holder)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("expected lock release event")
		}
	})

	t.Run("MultipleSubscribers", func(t *testing.T) {
		t.Parallel()
		b := newBus()
		ctx := t.Context()

		eventCh1 := make(chan LockReleasedEvent, 1)
		eventCh2 := make(chan LockReleasedEvent, 1)

		sub1 := b.onLockRelease.OnE(func(_ context.Context, evt LockReleasedEvent) error {
			eventCh1 <- evt
			return nil
		})
		defer sub1.Off()

		sub2 := b.onLockRelease.OnE(func(_ context.Context, evt LockReleasedEvent) error {
			eventCh2 <- evt
			return nil
		})
		defer sub2.Off()

		b.dispatchOnLockReleased(ctx, "domain", "stream", "consumer", "holder")

		select {
		case evt := <-eventCh1:
			assert.Equal(t, "domain", evt.Domain)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("expected event on channel 1")
		}

		select {
		case evt := <-eventCh2:
			assert.Equal(t, "domain", evt.Domain)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("expected event on channel 2")
		}
	})

	t.Run("UnsubscribeStopsEvents", func(t *testing.T) {
		t.Parallel()
		b := newBus()
		ctx := t.Context()

		callCount := 0
		sub := b.onLockRelease.OnE(func(_ context.Context, _ LockReleasedEvent) error {
			callCount++
			return nil
		})

		b.dispatchOnLockReleased(ctx, "domain", "stream", "consumer", "holder")
		assert.Equal(t, 1, callCount)

		sub.Off()
		time.Sleep(10 * time.Millisecond) // Allow unsubscribe to propagate

		b.dispatchOnLockReleased(ctx, "domain", "stream", "consumer", "holder")
		assert.Equal(t, 1, callCount, "should not receive events after unsubscribe")
	})
}
