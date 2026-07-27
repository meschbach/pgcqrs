package query2

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/meschbach/pgcqrs/pkg/ipc"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type mockWatchInternal struct {
	mu       sync.Mutex
	messages []*ipc.QueryOut
	errors   []error
	index    int
}

func (m *mockWatchInternal) Tick(_ context.Context) (*ipc.QueryOut, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.index >= len(m.messages) {
		if m.index < len(m.errors) {
			err := m.errors[m.index]
			m.index++
			return nil, err
		}
		return nil, context.Canceled
	}
	msg := m.messages[m.index]
	if m.index < len(m.errors) && m.errors[m.index] != nil {
		err := m.errors[m.index]
		m.index++
		return nil, err
	}
	m.index++
	return msg, nil
}

// blockingMockWatchInternal blocks on Tick until unblock is closed.
// Signals readiness via ready before blocking.
type blockingMockWatchInternal struct {
	ready   chan struct{}
	unblock chan struct{}
}

func (b *blockingMockWatchInternal) Tick(ctx context.Context) (*ipc.QueryOut, error) {
	close(b.ready)
	select {
	case <-b.unblock:
		return nil, context.Canceled
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func newMockMessage(id int64, kind string) *ipc.QueryOut {
	return &ipc.QueryOut{
		Op: 0,
		Id: &id,
		Envelope: &ipc.MaterializedEnvelope{
			Id:   id,
			When: timestamppb.Now(),
			Kind: kind,
		},
		Body: nil,
	}
}

func newTestWatch(messages []*ipc.QueryOut, errs []error) *Watch {
	mock := &mockWatchInternal{
		messages: messages,
		errors:   errs,
	}
	return &Watch{
		handlers: &handlers{
			registered: []v1.OnStreamQueryResult{
				func(_ context.Context, _ v1.Envelope, _ json.RawMessage) error {
					return nil
				},
			},
		},
		wirePump: mock,
	}
}

func TestTickWithIDReturnsEventID(t *testing.T) {
	t.Parallel()
	messages := []*ipc.QueryOut{
		newMockMessage(42, "ItemCreated"),
	}
	w := newTestWatch(messages, []error{nil, context.Canceled})

	ctx := t.Context()
	id, err := w.TickWithID(ctx)

	require.NoError(t, err)
	assert.Equal(t, int64(42), id)
}

func TestTickWithIDReturnsMultipleIDs(t *testing.T) {
	t.Parallel()
	messages := []*ipc.QueryOut{
		newMockMessage(10, "kind-a"),
		newMockMessage(20, "kind-b"),
		newMockMessage(30, "kind-c"),
	}
	w := newTestWatch(messages, []error{nil, nil, nil, context.Canceled})

	ctx := t.Context()
	id1, err := w.TickWithID(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(10), id1)

	id2, err := w.TickWithID(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(20), id2)

	id3, err := w.TickWithID(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(30), id3)
}

func TestTickWithIDHandlerErrorReturnsZeroID(t *testing.T) {
	t.Parallel()
	expectedErr := errors.New("handler failed")
	mock := &mockWatchInternal{
		messages: []*ipc.QueryOut{newMockMessage(99, "kind-a")},
		errors:   []error{nil},
	}
	w := &Watch{
		handlers: &handlers{
			registered: []v1.OnStreamQueryResult{
				func(_ context.Context, _ v1.Envelope, _ json.RawMessage) error {
					return expectedErr
				},
			},
		},
		wirePump: mock,
	}

	ctx := t.Context()
	id, err := w.TickWithID(ctx)

	require.ErrorIs(t, err, expectedErr)
	assert.Equal(t, int64(0), id)
}

func TestTickWithIDDispatchesToHandler(t *testing.T) {
	t.Parallel()
	messages := []*ipc.QueryOut{newMockMessage(55, "ItemUpdated")}
	var receivedEnvelope v1.Envelope
	mock := &mockWatchInternal{
		messages: messages,
		errors:   []error{nil, context.Canceled},
	}
	w := &Watch{
		handlers: &handlers{
			registered: []v1.OnStreamQueryResult{
				func(_ context.Context, e v1.Envelope, _ json.RawMessage) error {
					receivedEnvelope = e
					return nil
				},
			},
		},
		wirePump: mock,
	}

	ctx := t.Context()
	id, err := w.TickWithID(ctx)

	require.NoError(t, err)
	assert.Equal(t, int64(55), id)
	assert.Equal(t, int64(55), receivedEnvelope.ID)
	assert.Equal(t, "ItemUpdated", receivedEnvelope.Kind)
}

// --- Pump tests ---

func TestPumpReturnsHandlerError(t *testing.T) {
	t.Parallel()
	expectedErr := errors.New("handler failed")
	mock := &mockWatchInternal{
		messages: []*ipc.QueryOut{newMockMessage(1, "kind-a")},
		errors:   []error{nil, context.Canceled},
	}
	w := &Watch{
		handlers: &handlers{
			registered: []v1.OnStreamQueryResult{
				func(_ context.Context, _ v1.Envelope, _ json.RawMessage) error {
					return expectedErr
				},
			},
		},
		wirePump: mock,
	}

	err := w.Pump(t.Context())
	require.ErrorIs(t, err, expectedErr)
}

func TestPumpReturnsWireError(t *testing.T) {
	t.Parallel()
	expectedErr := errors.New("wire failed")
	mock := &mockWatchInternal{
		messages: []*ipc.QueryOut{},
		errors:   []error{expectedErr},
	}
	w := &Watch{
		handlers: &handlers{
			registered: []v1.OnStreamQueryResult{
				func(_ context.Context, _ v1.Envelope, _ json.RawMessage) error {
					return nil
				},
			},
		},
		wirePump: mock,
	}

	err := w.Pump(t.Context())
	require.ErrorIs(t, err, expectedErr)
}

func TestPumpProcessesAllEvents(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	count := 0
	mock := &mockWatchInternal{
		messages: []*ipc.QueryOut{
			newMockMessage(10, "kind-a"),
			newMockMessage(20, "kind-b"),
			newMockMessage(30, "kind-c"),
		},
		errors: []error{nil, nil, nil, context.Canceled},
	}
	w := &Watch{
		handlers: &handlers{
			registered: []v1.OnStreamQueryResult{
				func(_ context.Context, _ v1.Envelope, _ json.RawMessage) error {
					mu.Lock()
					count++
					mu.Unlock()
					return nil
				},
			},
		},
		wirePump: mock,
	}

	err := w.Pump(t.Context())
	require.ErrorIs(t, err, context.Canceled)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 3, count, "all three events should be processed before Pump stops")
}

func TestPumpStopsOnContextCancel(t *testing.T) {
	t.Parallel()
	ready := make(chan struct{})
	unblock := make(chan struct{})
	mock := &blockingMockWatchInternal{
		ready:   ready,
		unblock: unblock,
	}
	w := &Watch{
		handlers: &handlers{
			registered: []v1.OnStreamQueryResult{
				func(_ context.Context, _ v1.Envelope, _ json.RawMessage) error {
					return nil
				},
			},
		},
		wirePump: mock,
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- w.Pump(ctx)
	}()

	// Wait for the pump to enter Tick, then cancel
	<-ready
	cancel()
	close(unblock)

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Pump did not stop after context cancellation")
	}
}
