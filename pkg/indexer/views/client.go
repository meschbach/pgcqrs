package views

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/meschbach/pgcqrs/pkg/indexer"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
)

// Client handles the lifecycle of a view projection and exposes a query API.
type Client[L indexer.Lock] struct {
	projection *Projection
	store      Store
	reader     Reader
	notifier   *Notifier
	pump       *indexer.Pump[L]
	cancel     context.CancelFunc
	lastErr    atomic.Value
	errChan    chan error
}

// ProjectionClient is the interface for interacting with a running view projection.
type ProjectionClient interface {
	Get(ctx context.Context, kind string, key Key, opts ...GetOption) (*Entity, *Result, error)
	Version(ctx context.Context) (int64, error)
	OnChange(fn func(context.Context, Change) error) func()
	WaitForState(ctx context.Context, target indexer.PumpState) error
	Close() error
}

// ClientOption configures With.
type ClientOption interface {
	applyClient(*clientConfig)
}

type clientConfig struct {
	holder string
	ttl    time.Duration
}

type holderOption struct{ name string }

func (o holderOption) applyClient(c *clientConfig) { c.holder = o.name }

// WithHolder sets the holder identity for the consumer lock. Defaults to projection name.
func WithHolder(name string) ClientOption { return holderOption{name: name} }

// With creates a projection client running against the given system.
// The system's transport backs event sourcing and consumer locks; projection
// state is served through the transport's view connectivity (remote via gRPC,
// or in-process for memory systems).
func With(ctx context.Context, sys *v1.System, proj *Projection, opts ...ClientOption) (ProjectionClient, error) {
	transport := sys.Transport

	vf, ok := transport.(v1.ViewFeature)
	if !ok {
		return nil, fmt.Errorf("transport %T does not support view projections", transport)
	}
	backend, err := classifyConnectivity(vf.ViewConnectivity())
	if err != nil {
		return nil, fmt.Errorf("transport %T: %w", transport, err)
	}
	switch backend {
	case backendGRPC:
		return newGRPCClient(ctx, vf.ViewConnectivity().GRPC, transport, proj, opts)
	case backendMemory:
		return newMemoryClient(ctx, transport, proj, opts)
	default:
		return nil, fmt.Errorf("transport %T: unknown view backend %v", transport, backend)
	}
}

// viewBackend identifies which projection client backend serves a transport.
type viewBackend int

const (
	backendGRPC viewBackend = iota
	backendMemory
)

// classifyConnectivity maps a transport's view connectivity to the client
// backend that serves it. Transports exposing neither backend are an error so
// a future transport fails loudly instead of silently defaulting to memory.
func classifyConnectivity(c v1.ViewConnectivity) (viewBackend, error) {
	switch {
	case c.GRPC != nil:
		return backendGRPC, nil
	case c.Memory:
		return backendMemory, nil
	default:
		return 0, fmt.Errorf("transport exposes no view projection backend (grpc or memory)")
	}
}

// clientParts captures the transport-specific pieces a projection client needs.
// buildReader is deferred because the in-memory reader needs the Notifier that
// newClient creates; the gRPC reader ignores it.
type clientParts[L indexer.Lock] struct {
	wire        indexer.Wire[L]
	store       Store
	buildReader func(*Notifier) Reader
}

// newClient assembles a projection client from transport-specific parts and
// starts its pump against the given transport.
func newClient[L indexer.Lock](ctx context.Context, transport v1.Transport, proj *Projection, parts clientParts[L], opts []ClientOption) (ProjectionClient, error) {
	cfg := clientConfig{
		holder: proj.consumerName,
		ttl:    v1.DefaultLockTTL,
	}
	for _, opt := range opts {
		opt.applyClient(&cfg)
	}

	if err := transport.EnsureStream(ctx, proj.domain, proj.stream); err != nil {
		return nil, fmt.Errorf("ensure stream: %w", err)
	}

	sys := v1.NewSystem(transport)
	stream, err := sys.Stream(ctx, proj.domain, proj.stream)
	if err != nil {
		return nil, fmt.Errorf("create stream: %w", err)
	}

	notifier := NewNotifier()
	vi := NewIndexer(proj, parts.store, notifier, stream)
	pump := buildPump(parts.wire, vi, cfg.holder, cfg.ttl)
	reader := parts.buildReader(notifier)

	errChan := make(chan error, 1)
	pumpCtx, pumpCancel := context.WithCancel(ctx)
	client := &Client[L]{
		projection: proj,
		store:      parts.store,
		reader:     reader,
		notifier:   notifier,
		pump:       pump,
		cancel:     pumpCancel,
		errChan:    errChan,
	}

	go func() {
		defer pumpCancel()
		if err := pump.RunWithDomainStream(pumpCtx, proj.domain, proj.stream); err != nil {
			client.lastErr.Store(err)
			select {
			case errChan <- err:
			default:
			}
		}
		close(errChan)
	}()

	return client, nil
}

func buildPump[L indexer.Lock](wire indexer.Wire[L], idx indexer.Indexer, holder string, ttl time.Duration) *indexer.Pump[L] {
	return indexer.NewPump(wire, idx, holder, indexer.WithTTL(ttl))
}

// Get retrieves entity state with optional version constraints.
func (c *Client[L]) Get(ctx context.Context, kind string, key Key, opts ...GetOption) (*Entity, *Result, error) {
	entity, status, err := c.reader.Get(ctx, kind, key, opts...)
	if err != nil {
		return nil, nil, err
	}
	result := &Result{
		Entity: entity,
		Status: status,
	}
	return entity, result, nil
}

// Version returns the current projection version.
func (c *Client[L]) Version(ctx context.Context) (int64, error) {
	return c.reader.Version(ctx)
}

// OnChange registers a callback for change notifications.
// Returns an unsubscribe function that removes the callback when called.
func (c *Client[L]) OnChange(fn func(context.Context, Change) error) func() {
	return c.notifier.OnChange(fn)
}

// Close stops the Pump and releases resources.
func (c *Client[L]) Close() error {
	if c.cancel != nil {
		c.cancel()
	}
	return nil
}

// LastError returns the last error from the pump, or nil if no error has occurred.
// This is useful for checking if the pump has failed.
func (c *Client[L]) LastError() error {
	if v := c.lastErr.Load(); v != nil {
		if err, ok := v.(error); ok {
			return err
		}
	}
	return nil
}

// Errors returns a channel that receives pump errors.
// The channel is closed when the pump exits.
// This is useful for monitoring pump errors asynchronously.
func (c *Client[L]) Errors() <-chan error {
	return c.errChan
}

// State returns the current pump state.
func (c *Client[L]) State() indexer.PumpState {
	return c.pump.State()
}

// OnStateChange registers a callback to be invoked on every state transition.
// Returns an unsubscribe function.
func (c *Client[L]) OnStateChange(fn func(context.Context, indexer.PumpStateEvent) error) func() {
	return c.pump.OnStateChange(fn)
}

// WaitForState blocks until the pump reaches the target state or context is canceled.
func (c *Client[L]) WaitForState(ctx context.Context, target indexer.PumpState) error {
	return c.pump.WaitForState(ctx, target)
}
