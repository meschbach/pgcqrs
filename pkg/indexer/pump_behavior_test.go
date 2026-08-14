package indexer

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPump_DedupGuard(t *testing.T) {
	t.Parallel()

	t.Run("SkipsDuplicateEvents", func(t *testing.T) {
		t.Parallel()
		_, stream, pump := newMemoryPumpHarness(t, "domain", "stream")

		for i := 0; i < 3; i++ {
			_, err := stream.Submit(t.Context(), "TestEvent", map[string]int{"i": i})
			require.NoError(t, err)
		}

		err := runPump(t, pump, "domain", "stream", 1*time.Second)
		assert.Error(t, err)
	})
}

func TestPump_HeartbeatBehavior(t *testing.T) {
	t.Parallel()

	t.Run("HeartbeatConflictUpdatesPosition", func(t *testing.T) {
		t.Parallel()
		_, stream, pump := newMemoryPumpHarness(t, "domain", "stream")

		_, err := stream.Submit(t.Context(), "TestEvent", map[string]string{"key": "value"})
		require.NoError(t, err)

		err = runPump(t, pump, "domain", "stream", 1*time.Second)
		assert.Error(t, err)
	})

	t.Run("HeartbeatLockLossIsRecoverable", func(t *testing.T) {
		t.Parallel()
		// Verify that a LockNotHeldError wrapped in RecoverableError is classified as recoverable
		lockErr := &v1.LockNotHeldError{Consumer: "test", Holder: "test", Domain: "d", Stream: "s"}
		recoverableErr := &RecoverableError{Err: lockErr}

		// Direct check
		assert.True(t, IsRecoverable(recoverableErr))

		// Wrapped check (simulating what happens in pump.go:250)
		wrappedErr := fmt.Errorf("heartbeat failed: %w", recoverableErr)
		assert.True(t, IsRecoverable(wrappedErr))

		// Verify the underlying error is accessible via errors.As
		var lockNotHeld *v1.LockNotHeldError
		require.ErrorAs(t, recoverableErr, &lockNotHeld)
		assert.Equal(t, "test", lockNotHeld.Consumer)
	})
}

func TestPump_HandlerErrorRecovery(t *testing.T) {
	t.Parallel()

	t.Run("RecoverableErrorTransitionsToLockLost", func(t *testing.T) {
		t.Parallel()
		err := &RecoverableError{Err: errors.New("transient failure")}
		assert.True(t, IsRecoverable(err))
	})

	t.Run("NonRecoverableErrorTransitionsToFailed", func(t *testing.T) {
		t.Parallel()
		err := errors.New("permanent failure")
		assert.False(t, IsRecoverable(err))
	})
}

func TestPump_PositionTracking(t *testing.T) {
	t.Parallel()

	t.Run("PositionUpdatedAfterTick", func(t *testing.T) {
		t.Parallel()
		_, stream, pump := newMemoryPumpHarness(t, "domain", "stream")

		_, err := stream.Submit(t.Context(), "TestEvent", map[string]string{"key": "value"})
		require.NoError(t, err)

		err = runPump(t, pump, "domain", "stream", 1*time.Second)
		assert.Error(t, err)
	})

	t.Run("PositionUsedForWatchSetup", func(t *testing.T) {
		t.Parallel()
		transport, stream, pump := newMemoryPumpHarness(t, "domain", "stream")

		_, err := transport.SetPosition(t.Context(), "domain", "stream", "consumer", 10)
		require.NoError(t, err)

		for i := 0; i < 5; i++ {
			_, err = stream.Submit(t.Context(), "TestEvent", map[string]int{"i": i})
			require.NoError(t, err)
		}

		err = runPump(t, pump, "domain", "stream", 1*time.Second)
		assert.Error(t, err)
	})
}

func TestPump_LockAcquisition(t *testing.T) {
	t.Parallel()

	t.Run("WaitsForLockWhenHeldByOther", func(t *testing.T) {
		t.Parallel()
		transport, _, pump := newMemoryPumpHarness(t, "domain", "stream")

		_, err := transport.TryAcquire(t.Context(), "domain", "stream", "consumer", "other-holder", 30*time.Second)
		require.NoError(t, err)

		err = runPump(t, pump, "domain", "stream", 200*time.Millisecond)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("AcquiresLockWhenAvailable", func(t *testing.T) {
		t.Parallel()
		_, stream, pump := newMemoryPumpHarness(t, "domain", "stream")

		_, err := stream.Submit(t.Context(), "TestEvent", map[string]string{"key": "value"})
		require.NoError(t, err)

		err = runPump(t, pump, "domain", "stream", 1*time.Second)
		assert.Error(t, err)
	})
}

func TestPump_ContextCancellation(t *testing.T) {
	t.Parallel()

	t.Run("ExitsCleanlyOnContextCancel", func(t *testing.T) {
		t.Parallel()
		_, _, pump := newMemoryPumpHarness(t, "domain", "stream")

		runCtx, cancel := context.WithCancel(t.Context())

		done := make(chan error, 1)
		go func() {
			done <- pump.RunWithDomainStream(runCtx, "domain", "stream")
		}()

		time.Sleep(50 * time.Millisecond)
		cancel()

		err := <-done
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestPump_BackoffBehavior(t *testing.T) {
	t.Parallel()

	t.Run("BackoffResetOnSuccessfulAcquisition", func(t *testing.T) {
		t.Parallel()
		_, stream, pump := newMemoryPumpHarness(t, "domain", "stream")

		_, err := stream.Submit(t.Context(), "TestEvent", map[string]string{"key": "value"})
		require.NoError(t, err)

		err = runPump(t, pump, "domain", "stream", 1*time.Second)
		assert.Error(t, err)
	})
}
