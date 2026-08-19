package query2

import (
	"context"
	"errors"
	"fmt"
	"time"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
)

// Watch represents a continuous query that pumps results from a stream.
type Watch struct {
	handlers *handlers
	wirePump v1.WatchInternal
}

// Pump continuously ticks the watch until an error occurs or the context is canceled.
func (w *Watch) Pump(ctx context.Context) error {
	for {
		if err := w.Tick(ctx); err != nil {
			return err
		}
	}
}

// Tick performs a single iteration of the watch, processing one event if available.
func (w *Watch) Tick(ctx context.Context) error {
	_, err := w.TickWithID(ctx)
	return err
}

// TickWithID performs a single iteration of the watch, processing one event if
// available, and returns the event ID alongside the handler dispatch. This enables
// heartbeating with the event ID without dropping to WatchInternal.
func (w *Watch) TickWithID(ctx context.Context) (int64, error) {
	m, err := w.wirePump.Tick(ctx)
	if err != nil {
		return 0, err
	}
	if m == nil {
		return 0, errors.New("nil message on tick but not error")
	}
	var t string
	if m.Envelope != nil && m.Envelope.When != nil {
		t = m.Envelope.When.AsTime().Format(time.RFC3339)
	}
	if int(m.Op) < 0 || int(m.Op) >= len(w.handlers.registered) {
		return 0, fmt.Errorf("no handler registered for operation %d", m.Op)
	}
	handler := w.handlers.registered[m.Op]
	envelope := v1.Envelope{
		ID:   *m.Id,
		When: t,
		Kind: m.Envelope.Kind,
	}
	if err := handler(ctx, envelope, m.Body); err != nil {
		return 0, err
	}
	return *m.Id, nil
}
