package service

import (
	"context"

	"github.com/meschbach/go-junk-bucket/pkg/emitter"
)

// RequerySignal coordinates event notifications with a query loop.
// It ensures signals arriving during query execution are queued,
// not lost. Registration happens before the initial signal is sent.
type RequerySignal struct {
	ch    chan struct{}
	unsub func()
}

// NewRequerySignal creates a signal that is immediately ready (seeded)
// after registering with the emitter. The first Wait() returns without
// an emit. Subsequent Wait() calls block until Signal() fires.
func NewRequerySignal(_ context.Context, em *emitter.MutexDispatcher[EventStorageEvent]) *RequerySignal {
	rs := &RequerySignal{ch: make(chan struct{}, 1)}
	sub := em.OnE(func(_ context.Context, _ EventStorageEvent) error {
		select {
		case rs.ch <- struct{}{}:
		default:
		}
		return nil
	})
	rs.unsub = func() { em.Off(sub) }
	rs.ch <- struct{}{}
	return rs
}

// Wait blocks until a signal is received or context is canceled.
// Returns nil on signal, ctx.Err() on cancellation.
func (rs *RequerySignal) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-rs.ch:
		return nil
	}
}

// Signal notifies the consumer that a requery is needed.
// If the consumer is busy, the signal is queued (buffer 1).
// If already signaled, the signal coalesces (no-op).
func (rs *RequerySignal) Signal() {
	select {
	case rs.ch <- struct{}{}:
	default:
	}
}

// Close unregisters from the emitter. Safe to call multiple times.
func (rs *RequerySignal) Close() {
	if rs.unsub != nil {
		rs.unsub()
		rs.unsub = nil
	}
}
