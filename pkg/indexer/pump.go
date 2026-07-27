package indexer

import (
	"context"
	"errors"
	"fmt"
	"time"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/meschbach/pgcqrs/pkg/v1/query2"
)

// Pump drives the event processing loop for any Indexer.
// It acquires a consumer lock, resumes from the stored position, watches events,
// dispatches to handlers, and heartbeats position after each event.
type Pump struct {
	wire    Wire
	indexer Indexer
	holder  string
	opts    pumpOptions
}

type pumpOptions struct {
	ttl             time.Duration
	heartbeatMargin time.Duration
}

// Option configures the Pump.
type Option func(*pumpOptions)

// WithTTL sets the consumer lock TTL. Defaults to 30 seconds.
func WithTTL(ttl time.Duration) Option {
	return func(o *pumpOptions) { o.ttl = ttl }
}

// WithHeartbeatMargin sets the margin before heartbeat expiry to trigger renewal.
// Defaults to 200ms.
func WithHeartbeatMargin(margin time.Duration) Option {
	return func(o *pumpOptions) { o.heartbeatMargin = margin }
}

// NewPump creates a new Pump with the given Wire, Indexer, and holder identity.
func NewPump(wire Wire, indexer Indexer, holder string, opts ...Option) *Pump {
	o := pumpOptions{
		ttl:             v1.DefaultLockTTL,
		heartbeatMargin: 200 * time.Millisecond,
	}
	for _, opt := range opts {
		opt(&o)
	}
	return &Pump{
		wire:    wire,
		indexer: indexer,
		holder:  holder,
		opts:    o,
	}
}

// Run starts the event processing loop. It acquires a consumer lock, resumes from
// the stored position, and processes events until the context is canceled or an
// error occurs. Returns nil on context cancellation.
func (p *Pump) Run(ctx context.Context) error {
	return p.RunWithDomainStream(ctx, "", "")
}

// RunWithDomainStream starts the event processing loop for a specific domain and stream.
// If domain and stream are empty, the caller must ensure the Wire is pre-configured.
func (p *Pump) RunWithDomainStream(ctx context.Context, domain, stream string) error {
	keepAlive, watch, err := p.acquireAndSetup(ctx, domain, stream)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, keepAlive.Release(ctx))
	}()

	return p.runWatchLoop(ctx, watch, keepAlive)
}

func (p *Pump) acquireAndSetup(ctx context.Context, domain, stream string) (Lock, *query2.Watch, error) {
	consumer := p.holder
	ttl := p.opts.ttl

	// 1. Acquire consumer lock
	lockResult, err := p.wire.TryAcquire(ctx, domain, stream, consumer, p.holder, ttl)
	if err != nil {
		return nil, nil, fmt.Errorf("acquire lock: %w", err)
	}
	if !lockResult.Acquired {
		return nil, nil, fmt.Errorf("lock held by %s", lockResult.HeldBy)
	}

	// 2. Set up keep-alive for heartbeating
	keepAlive, err := p.wire.NewKeepAlive(ctx, domain, stream, consumer, p.holder)
	if err != nil {
		return nil, nil, fmt.Errorf("create keep-alive: %w", err)
	}

	// 3. Get stored position
	position, _, err := p.wire.GetPosition(ctx, domain, stream, consumer)
	if err != nil {
		return nil, nil, fmt.Errorf("get position: %w", err)
	}

	// 4. Build query from indexer
	query := p.indexer.Query()
	if position > 0 {
		query.After(position)
	}

	// 5. Start watch loop
	watch, err := query.Watch(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("start watch: %w", err)
	}

	return keepAlive, watch, nil
}

func (p *Pump) runWatchLoop(ctx context.Context, watch *query2.Watch, keepAlive Lock) error {
	for {
		eventID, err := watch.TickWithID(ctx)
		if err != nil {
			return err
		}
		if err := keepAlive.Heartbeat(ctx, eventID); err != nil {
			return fmt.Errorf("heartbeat: %w", err)
		}
	}
}
