package indexer

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/meschbach/go-junk-bucket/pkg/emitter"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/meschbach/pgcqrs/pkg/v1/query2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
)

// Pump drives the event processing loop for any Indexer.
// It acquires a consumer lock, resumes from the stored position, watches events,
// dispatches to handlers, and heartbeats position after each event.
type Pump[L Lock] struct {
	wire    Wire[L]
	indexer Indexer
	holder  string
	opts    pumpOptions

	state   atomic.Int64
	metrics *pumpMetrics
	onState *emitter.MutexDispatcher[PumpStateEvent]
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
func NewPump[L Lock](wire Wire[L], indexer Indexer, holder string, opts ...Option) *Pump[L] {
	o := pumpOptions{
		ttl:             v1.DefaultLockTTL,
		heartbeatMargin: 200 * time.Millisecond,
	}
	for _, opt := range opts {
		opt(&o)
	}
	p := &Pump[L]{
		wire:    wire,
		indexer: indexer,
		holder:  holder,
		opts:    o,
		metrics: newPumpMetrics(otel.Meter(meterName)),
		onState: emitter.NewMutexDispatcher[PumpStateEvent](),
	}
	p.state.Store(int64(PumpStateWaitingForLock))
	return p
}

// setState transitions the pump to a new state and emits a state change event.
// The state notification is decoupled from the caller's cancellation so that
// subscribers always observe the transition, even during shutdown.
func (p *Pump[L]) setState(ctx context.Context, newState PumpState, err error) {
	notifyCtx := context.WithoutCancel(ctx)

	prev := PumpState(p.state.Swap(int64(newState)))

	// Record state transition metric
	p.metrics.recordStateTransition(notifyCtx, newState)

	// Record lock loss events
	if newState == PumpStateLockLost {
		p.metrics.recordLockLossEvent(notifyCtx)
	}

	_, span := tracer.Start(ctx, "indexer.pump.setState")
	defer span.End()
	if err := p.onState.Emit(notifyCtx, PumpStateEvent{
		State:    newState,
		Previous: prev,
		Error:    err,
	}); err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		span.AddEvent("failure dispatching pump state change")
	}
}

// State returns the current pump state.
func (p *Pump[L]) State() PumpState {
	return PumpState(p.state.Load())
}

// OnStateChange registers a callback to be invoked on every state transition.
// Returns an unsubscribe function.
func (p *Pump[L]) OnStateChange(fn func(context.Context, PumpStateEvent) error) func() {
	sub := p.onState.OnE(fn)
	return func() {
		p.onState.Off(sub)
	}
}

// WaitForState blocks until the pump reaches the target state or context is canceled.
func (p *Pump[L]) WaitForState(ctx context.Context, target PumpState) error {
	if PumpState(p.state.Load()) == target {
		return nil
	}

	ch := make(chan struct{}, 1)
	unsub := p.OnStateChange(func(_ context.Context, evt PumpStateEvent) error {
		if evt.State == target {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
		return nil
	})
	defer unsub()

	// Check again in case state changed between initial check and subscription
	if PumpState(p.state.Load()) == target {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
		return nil
	}
}

// Run starts the event processing loop. It acquires a consumer lock, resumes from
// the stored position, and processes events until the context is canceled or an
// error occurs. Returns nil on context cancellation.
func (p *Pump[L]) Run(ctx context.Context) error {
	return p.RunWithDomainStream(ctx, "", "")
}

// RunWithDomainStream starts the event processing loop for a specific domain and stream.
// If domain and stream are empty, the caller must ensure the Wire is pre-configured.
func (p *Pump[L]) RunWithDomainStream(ctx context.Context, domain, stream string) error {
	// Start in WaitingForLock state
	p.setState(ctx, PumpStateWaitingForLock, nil)

	// Monitor context cancellation
	go func() {
		<-ctx.Done()
		p.setState(ctx, PumpStateClosed, ctx.Err())
	}()

	backoff := NewBackoff()

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		retry, err := p.runPumpTick(ctx, domain, stream)
		if err == nil {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if retry {
			p.setState(ctx, PumpStateLockLost, err)
			time.Sleep(backoff.NextBackOff())
			continue
		}
		p.setState(ctx, PumpStateFailed, err)
		return err
	}
}

// runPumpTick performs one acquire-watch-release cycle. The retry result tells
// the outer loop whether to back off and retry after a recoverable failure, or
// whether the cycle failed permanently and RunWithDomainStream should exit.
func (p *Pump[L]) runPumpTick(ctx context.Context, domain, stream string) (retry bool, retErr error) {
	// Transition to Acquiring state
	p.setState(ctx, PumpStateAcquiring, nil)

	// Acquire consumer lock and get position
	lock, lockPosition, err := p.wire.WaitForLock(ctx, domain, stream, p.holder, p.holder, p.opts.ttl)
	if err != nil {
		return true, err
	}
	defer func() { retErr = errors.Join(retErr, lock.Release(ctx)) }()

	// Build query from indexer
	query := p.indexer.Query()
	if lockPosition > 0 {
		query.After(lockPosition)
	}

	// Start watch loop
	watch, err := query.Watch(ctx)
	if err != nil {
		return true, err
	}

	// Transition to Watching state
	p.setState(ctx, PumpStateWatching, nil)

	position := lockPosition
	err = p.runWatchLoop(ctx, watch, lock, &position)
	if err != nil {
		// Record watch stream failure
		p.metrics.recordWatchStreamFailure(ctx)
		return IsRecoverable(err), err
	}
	return false, nil
}

func (p *Pump[L]) runWatchLoop(ctx context.Context, watch *query2.Watch, keepAlive L, position *int64) error {
	for {
		eventID, err := watch.TickWithID(ctx)
		if err != nil {
			return err
		}

		// Dedup guard: skip events we've already processed
		if eventID <= *position {
			continue
		}

		// Update position after successful tick
		*position = eventID

		// Heartbeat with current position
		if err := keepAlive.Heartbeat(ctx, *position); err != nil {
			// Check for HeartbeatConflictError - update position and continue
			var conflictErr *v1.HeartbeatConflictError
			if errors.As(err, &conflictErr) {
				*position = conflictErr.CurrentVersion
				continue
			}
			// Any other heartbeat error is a lock loss
			return fmt.Errorf("heartbeat: %w", err)
		}
	}
}
