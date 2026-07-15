package query2

import (
	"context"
	"errors"
	"iter"
	"time"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
)

// Watch represents a continuous query that pumps results from a stream.
type Watch struct {
	handlers *handlers
	wirePump v1.WatchInternal
	err      error
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
// todo: consider other locations
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

// ChannelOption configures the behavior of Watch.Channel.
type ChannelOption interface {
	apply(*channelConfig)
}

type channelConfig struct {
	blocking bool
}

type nonBlockingOption struct{}

func (nonBlockingOption) apply(c *channelConfig) { c.blocking = false }

// WithNonBlocking configures Channel to drop events when the buffer is full
// instead of blocking. The default behavior is blocking (backpressure).
func WithNonBlocking() ChannelOption { return nonBlockingOption{} }

// Channel returns a buffered channel that receives envelopes from the watch.
// A hidden goroutine runs Tick() and feeds the channel. The channel is closed
// when Tick() returns an error or the context is canceled. Use Err() after
// the channel is closed to check for errors.
// If backlog is 0, a default of 16 is used.
// By default, the goroutine blocks when the channel buffer is full (backpressure).
// Pass WithNonBlocking() to drop events when the buffer is full.
func (w *Watch) Channel(ctx context.Context, backlog int, opts ...ChannelOption) <-chan v1.Envelope {
	if backlog <= 0 {
		backlog = 16
	}
	cfg := &channelConfig{blocking: true}
	for _, opt := range opts {
		opt.apply(cfg)
	}
	ch := make(chan v1.Envelope, backlog)
	go w.channelPump(ctx, ch, cfg)
	return ch
}

func (w *Watch) channelPump(ctx context.Context, ch chan<- v1.Envelope, cfg *channelConfig) {
	defer close(ch)
	for {
		envelope, err := w.nextEnvelope(ctx)
		if err != nil {
			return
		}
		if !w.sendEnvelope(ctx, ch, envelope, cfg) {
			return
		}
	}
}

func (w *Watch) nextEnvelope(ctx context.Context) (v1.Envelope, error) {
	m, err := w.wirePump.Tick(ctx)
	if err != nil {
		w.err = err
		return v1.Envelope{}, err
	}
	if m == nil {
		w.err = errors.New("nil message on tick but not error")
		return v1.Envelope{}, w.err
	}
	var t string
	if m.Envelope != nil && m.Envelope.When != nil {
		t = m.Envelope.When.AsTime().Format(time.RFC3339)
	}
	return v1.Envelope{
		ID:   *m.Id,
		When: t,
		Kind: m.Envelope.Kind,
	}, nil
}

func (w *Watch) sendEnvelope(ctx context.Context, ch chan<- v1.Envelope, envelope v1.Envelope, cfg *channelConfig) bool {
	if cfg.blocking {
		select {
		case ch <- envelope:
			return true
		case <-ctx.Done():
			return false
		}
	}
	select {
	case ch <- envelope:
	default:
	}
	return true
}

// Err returns the error that caused the channel to close (nil if closed cleanly
// via context cancellation). Must be called after the channel is drained/closed.
func (w *Watch) Err() error {
	return w.err
}

// Events returns a pull-based iterator that yields envelopes from the watch.
// No internal goroutine is created beyond the existing Tick() pump.
// Yields Envelope values until Tick() returns an error.
func (w *Watch) Events(ctx context.Context) iter.Seq2[v1.Envelope, error] {
	return func(yield func(v1.Envelope, error) bool) {
		for {
			m, err := w.wirePump.Tick(ctx)
			if err != nil {
				yield(v1.Envelope{}, err)
				return
			}
			if m == nil {
				yield(v1.Envelope{}, errors.New("nil message on tick but not error"))
				return
			}
			var t string
			if m.Envelope != nil && m.Envelope.When != nil {
				t = m.Envelope.When.AsTime().Format(time.RFC3339)
			}
			envelope := v1.Envelope{
				ID:   *m.Id,
				When: t,
				Kind: m.Envelope.Kind,
			}
			if !yield(envelope, nil) {
				return
			}
		}
	}
}
