package indexer

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPumpState_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state    PumpState
		expected string
	}{
		{PumpStateWaitingForLock, "WaitingForLock"},
		{PumpStateAcquiring, "Acquiring"},
		{PumpStateWatching, "Watching"},
		{PumpStateLockLost, "LockLost"},
		{PumpStateFailed, "Failed"},
		{PumpStateClosed, "Closed"},
		{PumpState(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.state.String())
		})
	}
}

func TestPumpStateEvent(t *testing.T) {
	t.Parallel()

	t.Run("EventContainsStateTransition", func(t *testing.T) {
		t.Parallel()
		evt := PumpStateEvent{
			State:    PumpStateWatching,
			Previous: PumpStateAcquiring,
			Error:    nil,
		}
		assert.Equal(t, PumpStateWatching, evt.State)
		assert.Equal(t, PumpStateAcquiring, evt.Previous)
		assert.NoError(t, evt.Error)
	})

	t.Run("EventContainsError", func(t *testing.T) {
		t.Parallel()
		err := assert.AnError
		evt := PumpStateEvent{
			State:    PumpStateFailed,
			Previous: PumpStateWatching,
			Error:    err,
		}
		assert.ErrorIs(t, evt.Error, err)
	})
}

func TestPump_StateObservability(t *testing.T) {
	t.Parallel()

	t.Run("InitialStateIsWaitingForLock", func(t *testing.T) {
		t.Parallel()
		transport := newMockTransport()
		wire := newMockWire(transport)
		indexer := newMockIndexer(nil)
		pump := NewPump(wire, indexer, "test-holder")

		assert.Equal(t, PumpStateWaitingForLock, pump.State())
	})

	t.Run("OnStateChangeReceivesTransitions", func(t *testing.T) {
		t.Parallel()
		transport := newMockTransport()
		wire := newMockWire(transport)
		indexer := newMockIndexer(nil)
		pump := NewPump(wire, indexer, "test-holder")

		var events []PumpStateEvent
		unsub := pump.OnStateChange(func(_ context.Context, evt PumpStateEvent) error {
			events = append(events, evt)
			return nil
		})
		defer unsub()

		pump.setState(t.Context(), PumpStateAcquiring, nil)
		pump.setState(t.Context(), PumpStateWatching, nil)

		require.Len(t, events, 2)
		assert.Equal(t, PumpStateAcquiring, events[0].State)
		assert.Equal(t, PumpStateWaitingForLock, events[0].Previous)
		assert.Equal(t, PumpStateWatching, events[1].State)
		assert.Equal(t, PumpStateAcquiring, events[1].Previous)
	})

	t.Run("OnStateChangeReturnsUnsubscribe", func(t *testing.T) {
		t.Parallel()
		transport := newMockTransport()
		wire := newMockWire(transport)
		indexer := newMockIndexer(nil)
		pump := NewPump(wire, indexer, "test-holder")

		unsub := pump.OnStateChange(func(_ context.Context, _ PumpStateEvent) error {
			return nil
		})

		assert.NotNil(t, unsub, "should return unsubscribe function")
		unsub()
	})

	t.Run("WaitForState_ReturnsImmediatelyIfAlreadyInState", func(t *testing.T) {
		t.Parallel()
		transport := newMockTransport()
		wire := newMockWire(transport)
		indexer := newMockIndexer(nil)
		pump := NewPump(wire, indexer, "test-holder")

		ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
		defer cancel()

		err := pump.WaitForState(ctx, PumpStateWaitingForLock)
		require.NoError(t, err)
	})

	t.Run("WaitForState_BlocksUntilTargetState", func(t *testing.T) {
		t.Parallel()
		transport := newMockTransport()
		wire := newMockWire(transport)
		indexer := newMockIndexer(nil)
		pump := NewPump(wire, indexer, "test-holder")

		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()

		done := make(chan error, 1)
		go func() {
			done <- pump.WaitForState(ctx, PumpStateWatching)
		}()

		time.Sleep(50 * time.Millisecond)
		pump.setState(t.Context(), PumpStateAcquiring, nil)
		pump.setState(t.Context(), PumpStateWatching, nil)

		err := <-done
		require.NoError(t, err)
	})

	t.Run("WaitForState_ReturnsContextError", func(t *testing.T) {
		t.Parallel()
		transport := newMockTransport()
		wire := newMockWire(transport)
		indexer := newMockIndexer(nil)
		pump := NewPump(wire, indexer, "test-holder")

		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()

		err := pump.WaitForState(ctx, PumpStateWatching)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})
}
