package query2

import (
	"context"
	"errors"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"time"
)

type Watch struct {
	handlers *handlers
	wirePump v1.WatchInternal
}

func (w *Watch) Pump(ctx context.Context) error {
	for {
		if err := w.Tick(ctx); err != nil {
			return err
		}
	}
}

func (w *Watch) Tick(ctx context.Context) error {
	m, err := w.wirePump.Tick(ctx)
	if err != nil {
		return err
	}
	if m == nil {
		return errors.New("nil message on tick but not error")
	}
	var t string
	if m.Envelope != nil && m.Envelope.When != nil {
		t = m.Envelope.When.AsTime().Format(time.RFC3339)
	}
	handler := w.handlers.registered[m.Op]
	envelope := v1.Envelope{
		ID:   *m.Id,
		When: t,
		Kind: m.Envelope.Kind,
	}
	if err := handler(ctx, envelope, m.Body); err != nil {
		return err
	}
	return nil
}
