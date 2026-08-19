package service

import (
	"context"
	"testing"
	"time"

	"github.com/meschbach/go-junk-bucket/pkg/emitter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequerySignalSeedArrivesImmediately(t *testing.T) {
	t.Parallel()
	em := emitter.NewMutexDispatcher[EventStorageEvent]()
	ctx := t.Context()

	signal := NewRequerySignal(ctx, em)
	defer signal.Close()

	done := make(chan error, 1)
	go func() {
		done <- signal.Wait(ctx)
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Wait should return immediately from seed")
	}
}

func TestRequerySignalQueuesDuringBusyPeriod(t *testing.T) {
	t.Parallel()
	em := emitter.NewMutexDispatcher[EventStorageEvent]()
	ctx := t.Context()

	signal := NewRequerySignal(ctx, em)
	defer signal.Close()

	// Consume the seed
	done := make(chan error, 1)
	go func() {
		done <- signal.Wait(ctx)
	}()
	<-done

	// Now call Wait again (simulating "busy" period where we're processing)
	go func() {
		done <- signal.Wait(ctx)
	}()

	// Give goroutine time to enter Wait
	time.Sleep(10 * time.Millisecond)

	// Emit event while "busy" (Wait is waiting)
	require.NoError(t, em.Emit(ctx, EventStorageEvent{}))

	// Wait should return because signal was queued
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Wait should return after queued signal")
	}
}

func TestRequerySignalMultipleSignalsCoalesce(t *testing.T) {
	t.Parallel()
	em := emitter.NewMutexDispatcher[EventStorageEvent]()
	ctx := t.Context()

	signal := NewRequerySignal(ctx, em)
	defer signal.Close()

	// Consume the seed
	done := make(chan error, 1)
	go func() {
		done <- signal.Wait(ctx)
	}()
	<-done

	// Emit 5 events rapidly
	for i := 0; i < 5; i++ {
		require.NoError(t, em.Emit(ctx, EventStorageEvent{}))
	}

	// Wait should return once (signals coalesced)
	go func() {
		done <- signal.Wait(ctx)
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Wait should return once after coalesced signals")
	}

	// Verify no more signals pending
	select {
	case <-done:
		t.Fatal("Should not have a second signal")
	case <-time.After(50 * time.Millisecond):
		// Expected: no more signals
	}
}

func TestRequerySignalContextCancellationUnblocksWait(t *testing.T) {
	t.Parallel()
	em := emitter.NewMutexDispatcher[EventStorageEvent]()

	signal := NewRequerySignal(t.Context(), em)
	defer signal.Close()

	// Consume the seed
	ctx := t.Context()
	done := make(chan error, 1)
	go func() {
		done <- signal.Wait(ctx)
	}()
	<-done

	// Create cancellable context for next Wait
	waitCtx, cancel := context.WithCancel(ctx)

	go func() {
		done <- signal.Wait(waitCtx)
	}()

	// Give goroutine time to enter Wait
	time.Sleep(10 * time.Millisecond)

	// Cancel context
	cancel()

	// Wait should return with context error
	select {
	case err := <-done:
		require.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Wait should return on context cancellation")
	}
}

func TestRequerySignalCloseCleansUp(t *testing.T) {
	t.Parallel()
	em := emitter.NewMutexDispatcher[EventStorageEvent]()
	ctx := t.Context()

	signal := NewRequerySignal(ctx, em)

	// Close should not panic
	signal.Close()

	// Double close should not panic
	signal.Close()

	// Emit after close should not panic (listener removed)
	require.NoError(t, em.Emit(ctx, EventStorageEvent{}))
}

func TestRequerySignalWaitReturnsContextError(t *testing.T) {
	t.Parallel()
	em := emitter.NewMutexDispatcher[EventStorageEvent]()

	signal := NewRequerySignal(t.Context(), em)
	defer signal.Close()

	// Consume the seed
	ctx := t.Context()
	done := make(chan error, 1)
	go func() {
		done <- signal.Wait(ctx)
	}()
	<-done

	// Create already-canceled context
	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()

	// Wait should return immediately with context error
	go func() {
		done <- signal.Wait(cancelledCtx)
	}()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Wait should return immediately on canceled context")
	}
}
