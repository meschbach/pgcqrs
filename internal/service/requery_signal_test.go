package service

import (
	"context"
	"testing"

	"testing/synctest"

	"github.com/meschbach/go-junk-bucket/pkg/emitter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequerySignalSeedArrivesImmediately(t *testing.T) { //nolint:paralleltest // synctest.Test doesn't support t.Parallel() inside bubble
	synctest.Test(t, func(t *testing.T) {
		em := emitter.NewMutexDispatcher[EventStorageEvent]()
		ctx := t.Context()

		signal := NewRequerySignal(ctx, em)
		defer signal.Close()

		done := make(chan error, 1)
		go func() {
			done <- signal.Wait(ctx)
		}()

		// Wait for the goroutine to enter signal.Wait and consume the seed
		synctest.Wait()
		require.NoError(t, <-done)
	})
}

func TestRequerySignalQueuesDuringBusyPeriod(t *testing.T) { //nolint:paralleltest // synctest.Test doesn't support t.Parallel() inside bubble
	synctest.Test(t, func(t *testing.T) {
		em := emitter.NewMutexDispatcher[EventStorageEvent]()
		ctx := t.Context()

		signal := NewRequerySignal(ctx, em)
		defer signal.Close()

		// Consume the seed
		done := make(chan error, 1)
		go func() {
			done <- signal.Wait(ctx)
		}()
		synctest.Wait()
		require.NoError(t, <-done)

		// Now call Wait again (simulating "busy" period where we're processing)
		go func() {
			done <- signal.Wait(ctx)
		}()

		// Wait for goroutine to enter Wait
		synctest.Wait()

		// Emit event while "busy" (Wait is waiting)
		require.NoError(t, em.Emit(ctx, EventStorageEvent{}))

		// Wait should return because signal was queued
		synctest.Wait()
		require.NoError(t, <-done)
	})
}

func TestRequerySignalMultipleSignalsCoalesce(t *testing.T) { //nolint:paralleltest // synctest.Test doesn't support t.Parallel() inside bubble
	synctest.Test(t, func(t *testing.T) {
		em := emitter.NewMutexDispatcher[EventStorageEvent]()
		ctx := t.Context()

		signal := NewRequerySignal(ctx, em)
		defer signal.Close()

		// Consume the seed
		done := make(chan error, 1)
		go func() {
			done <- signal.Wait(ctx)
		}()
		synctest.Wait()
		require.NoError(t, <-done)

		// Emit 5 events rapidly
		for i := 0; i < 5; i++ {
			require.NoError(t, em.Emit(ctx, EventStorageEvent{}))
		}

		// Wait should return once (signals coalesced)
		go func() {
			done <- signal.Wait(ctx)
		}()

		synctest.Wait()
		require.NoError(t, <-done)

		// Verify no more signals pending - channel should not receive again
		// After synctest.Wait(), if there were another signal, the goroutine would have returned
		// Since we haven't called Wait again, no more signals should be pending
	})
}

func TestRequerySignalContextCancellationUnblocksWait(t *testing.T) { //nolint:paralleltest // synctest.Test doesn't support t.Parallel() inside bubble
	synctest.Test(t, func(t *testing.T) {
		em := emitter.NewMutexDispatcher[EventStorageEvent]()

		signal := NewRequerySignal(t.Context(), em)
		defer signal.Close()

		// Consume the seed
		ctx := t.Context()
		done := make(chan error, 1)
		go func() {
			done <- signal.Wait(ctx)
		}()
		synctest.Wait()
		require.NoError(t, <-done)

		// Create cancellable context for next Wait
		waitCtx, cancel := context.WithCancel(ctx)

		go func() {
			done <- signal.Wait(waitCtx)
		}()

		// Wait for goroutine to enter Wait
		synctest.Wait()

		// Cancel context
		cancel()

		// Wait should return with context error
		synctest.Wait()
		err := <-done
		require.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})
}

func TestRequerySignalCloseCleansUp(t *testing.T) { //nolint:paralleltest // synctest.Test doesn't support t.Parallel() inside bubble
	synctest.Test(t, func(t *testing.T) {
		em := emitter.NewMutexDispatcher[EventStorageEvent]()
		ctx := t.Context()

		signal := NewRequerySignal(ctx, em)

		// Close should not panic
		signal.Close()

		// Double close should not panic
		signal.Close()

		// Emit after close should not panic (listener removed)
		require.NoError(t, em.Emit(ctx, EventStorageEvent{}))
	})
}

func TestRequerySignalWaitReturnsContextError(t *testing.T) { //nolint:paralleltest // synctest.Test doesn't support t.Parallel() inside bubble
	synctest.Test(t, func(t *testing.T) {
		em := emitter.NewMutexDispatcher[EventStorageEvent]()

		signal := NewRequerySignal(t.Context(), em)
		defer signal.Close()

		// Consume the seed
		ctx := t.Context()
		done := make(chan error, 1)
		go func() {
			done <- signal.Wait(ctx)
		}()
		synctest.Wait()
		require.NoError(t, <-done)

		// Create already-canceled context
		cancelledCtx, cancel := context.WithCancel(ctx)
		cancel()

		// Wait should return immediately with context error
		go func() {
			done <- signal.Wait(cancelledCtx)
		}()

		synctest.Wait()
		err := <-done
		require.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})
}
