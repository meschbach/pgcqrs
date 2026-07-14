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

func TestChannelEventsFlowThrough(t *testing.T) {
	t.Parallel()
	messages := []*ipc.QueryOut{
		newMockMessage(1, "kind-a"),
		newMockMessage(2, "kind-b"),
		newMockMessage(3, "kind-a"),
	}
	w := newTestWatch(messages, []error{nil, nil, nil, context.Canceled})

	ctx := t.Context()
	ch := w.Channel(ctx, 4)
	var received []v1.Envelope
	for env := range ch {
		received = append(received, env)
	}

	require.Len(t, received, 3)
	assert.Equal(t, int64(1), received[0].ID)
	assert.Equal(t, "kind-a", received[0].Kind)
	assert.Equal(t, int64(2), received[1].ID)
	assert.Equal(t, "kind-b", received[1].Kind)
	assert.Equal(t, int64(3), received[2].ID)
	assert.Equal(t, "kind-a", received[2].Kind)
}

func TestChannelErrorClosesChannel(t *testing.T) {
	t.Parallel()
	expectedErr := errors.New("test error")
	messages := []*ipc.QueryOut{
		newMockMessage(1, "kind-a"),
	}
	w := newTestWatch(messages, []error{nil, expectedErr})

	ctx := t.Context()
	ch := w.Channel(ctx, 4)
	var received []v1.Envelope
	for env := range ch {
		received = append(received, env)
	}

	require.Len(t, received, 1)
	assert.Equal(t, int64(1), received[0].ID)
	assert.Equal(t, expectedErr, w.Err())
}

func TestChannelContextCancellation(t *testing.T) {
	t.Parallel()
	messages := []*ipc.QueryOut{
		newMockMessage(1, "kind-a"),
		newMockMessage(2, "kind-b"),
	}
	w := newTestWatch(messages, []error{nil, nil, context.Canceled})

	ctx := t.Context()
	ch := w.Channel(ctx, 4)
	var received []v1.Envelope
	for env := range ch {
		received = append(received, env)
		if len(received) >= 2 {
			break
		}
	}

	require.Len(t, received, 2)
}

func TestChannelDefaultBacklog(t *testing.T) {
	t.Parallel()
	messages := []*ipc.QueryOut{
		newMockMessage(1, "kind-a"),
	}
	w := newTestWatch(messages, []error{nil, context.Canceled})

	ctx := t.Context()
	ch := w.Channel(ctx, 0)
	var received []v1.Envelope
	for env := range ch {
		received = append(received, env)
	}

	require.Len(t, received, 1)
}

func TestChannelBlockingDefault(t *testing.T) {
	t.Parallel()
	messages := []*ipc.QueryOut{
		newMockMessage(1, "kind-a"),
		newMockMessage(2, "kind-b"),
		newMockMessage(3, "kind-c"),
		newMockMessage(4, "kind-d"),
		newMockMessage(5, "kind-e"),
	}
	w := newTestWatch(messages, []error{nil, nil, nil, nil, nil, context.Canceled})

	ctx := t.Context()
	ch := w.Channel(ctx, 1)
	var received []v1.Envelope
	for env := range ch {
		received = append(received, env)
	}

	require.Len(t, received, 5)
	assert.Equal(t, int64(1), received[0].ID)
	assert.Equal(t, int64(5), received[4].ID)
}

func TestChannelNonBlockingDropsEvents(t *testing.T) {
	t.Parallel()
	messages := []*ipc.QueryOut{
		newMockMessage(1, "kind-a"),
		newMockMessage(2, "kind-b"),
		newMockMessage(3, "kind-c"),
		newMockMessage(4, "kind-d"),
		newMockMessage(5, "kind-e"),
	}
	w := newTestWatch(messages, []error{nil, nil, nil, nil, nil, context.Canceled})

	ctx := t.Context()
	ch := w.Channel(ctx, 1, WithNonBlocking())
	time.Sleep(50 * time.Millisecond)
	var received []v1.Envelope
	timeout := time.After(100 * time.Millisecond)
	done := false
	for !done {
		select {
		case env, ok := <-ch:
			if !ok {
				done = true
			} else {
				received = append(received, env)
			}
		case <-timeout:
			done = true
		}
	}

	require.Less(t, len(received), 5)
}

func TestEventsYieldsEvents(t *testing.T) {
	t.Parallel()
	messages := []*ipc.QueryOut{
		newMockMessage(1, "kind-a"),
		newMockMessage(2, "kind-b"),
		newMockMessage(3, "kind-c"),
	}
	w := newTestWatch(messages, []error{nil, nil, nil, context.Canceled})

	ctx := t.Context()
	var received []v1.Envelope
	for env, err := range w.Events(ctx) {
		if err != nil {
			break
		}
		received = append(received, env)
	}

	require.Len(t, received, 3)
	assert.Equal(t, int64(1), received[0].ID)
	assert.Equal(t, "kind-a", received[0].Kind)
	assert.Equal(t, int64(2), received[1].ID)
	assert.Equal(t, "kind-b", received[1].Kind)
	assert.Equal(t, int64(3), received[2].ID)
	assert.Equal(t, "kind-c", received[2].Kind)
}

func TestEventsErrorTerminatesIteration(t *testing.T) {
	t.Parallel()
	expectedErr := errors.New("test error")
	messages := []*ipc.QueryOut{
		newMockMessage(1, "kind-a"),
	}
	w := newTestWatch(messages, []error{nil, expectedErr})

	ctx := t.Context()
	var received []v1.Envelope
	var finalErr error
	for env, err := range w.Events(ctx) {
		if err != nil {
			finalErr = err
			break
		}
		received = append(received, env)
	}

	require.Len(t, received, 1)
	assert.Equal(t, int64(1), received[0].ID)
	assert.Equal(t, expectedErr, finalErr)
}

func TestEventsContextCancellation(t *testing.T) {
	t.Parallel()
	messages := []*ipc.QueryOut{
		newMockMessage(1, "kind-a"),
		newMockMessage(2, "kind-b"),
	}
	w := newTestWatch(messages, []error{nil, nil, context.Canceled})

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	var received []v1.Envelope
	for env, err := range w.Events(ctx) {
		if err != nil {
			break
		}
		received = append(received, env)
	}

	require.Len(t, received, 2)
}

func TestEventsEarlyBreak(t *testing.T) {
	t.Parallel()
	messages := []*ipc.QueryOut{
		newMockMessage(1, "kind-a"),
		newMockMessage(2, "kind-b"),
		newMockMessage(3, "kind-c"),
	}
	w := newTestWatch(messages, []error{nil, nil, nil, context.Canceled})

	ctx := t.Context()
	var received []v1.Envelope
	for env, err := range w.Events(ctx) {
		if err != nil {
			break
		}
		received = append(received, env)
		if len(received) >= 2 {
			break
		}
	}

	require.Len(t, received, 2)
	assert.Equal(t, int64(1), received[0].ID)
	assert.Equal(t, int64(2), received[1].ID)
}
