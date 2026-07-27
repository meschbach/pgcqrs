package views

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/meschbach/pgcqrs/pkg/indexer"
	vgrpc "github.com/meschbach/pgcqrs/pkg/indexer/views/grpc"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Client handles the lifecycle of a view projection and exposes a query API.
type Client struct {
	projection *Projection
	store      Store
	notifier   *Notifier
	pump       *indexer.Pump
	cancel     context.CancelFunc
	consumer   vgrpc.ViewProjectionConsumerClient
	lastErr    atomic.Value
	errChan    chan error
}

// ConnectOption configures Connect.
type ConnectOption interface {
	applyConnect(*connectConfig)
}

type connectConfig struct {
	holder string
	ttl    time.Duration
}

type holderOption struct{ name string }

func (o holderOption) applyConnect(c *connectConfig) { c.holder = o.name }

// WithHolder sets the holder identity for the consumer lock. Defaults to projection name.
func WithHolder(name string) ConnectOption { return holderOption{name: name} }

// Connect to pgcqrs service, build Wire + Transport + Pump, start processing.
func Connect(ctx context.Context, address string, proj *Projection, opts ...ConnectOption) (*Client, error) {
	cfg := connectConfig{
		holder: proj.consumerName,
		ttl:    v1.DefaultLockTTL,
	}
	for _, opt := range opts {
		opt.applyConnect(&cfg)
	}

	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}

	transport := v1.NewGrpcAdapter(conn)
	wire := indexer.GrpcWireConn(conn)

	if err := transport.EnsureStream(ctx, proj.domain, proj.stream); err != nil {
		return nil, fmt.Errorf("ensure stream: %w", err)
	}

	sys := v1.NewSystem(transport)
	stream, err := sys.Stream(ctx, proj.domain, proj.stream)
	if err != nil {
		return nil, fmt.Errorf("create stream: %w", err)
	}

	store := NewRemoteStore(conn, NewProjectionIdentity(proj.domain, proj.stream, proj.consumerName))
	notifier := NewNotifier()
	vi := NewIndexer(proj, store, notifier, stream)
	pump := buildPump(wire, vi, cfg.holder, cfg.ttl)
	consumer := vgrpc.NewViewProjectionConsumerClient(conn)

	errChan := make(chan error, 1)
	pumpCtx, pumpCancel := context.WithCancel(ctx)
	client := &Client{
		projection: proj,
		store:      store,
		notifier:   notifier,
		pump:       pump,
		cancel:     pumpCancel,
		consumer:   consumer,
		errChan:    errChan,
	}

	go func() {
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

// ConnectMemory creates a Client with in-memory transport (for testing).
func ConnectMemory(ctx context.Context, transport v1.Transport, proj *Projection) (*Client, error) {
	wire := indexer.MemoryWire(transport)

	if err := transport.EnsureStream(ctx, proj.domain, proj.stream); err != nil {
		return nil, fmt.Errorf("ensure stream: %w", err)
	}

	sys := v1.NewSystem(transport)
	stream, err := sys.Stream(ctx, proj.domain, proj.stream)
	if err != nil {
		return nil, fmt.Errorf("create stream: %w", err)
	}

	store := NewMemoryStore()
	notifier := NewNotifier()
	vi := NewIndexer(proj, store, notifier, stream)
	pump := buildPump(wire, vi, proj.consumerName, v1.DefaultLockTTL)

	errChan := make(chan error, 1)
	pumpCtx, pumpCancel := context.WithCancel(ctx)
	client := &Client{
		projection: proj,
		store:      store,
		notifier:   notifier,
		pump:       pump,
		cancel:     pumpCancel,
		errChan:    errChan,
	}

	go func() {
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

func buildPump(wire indexer.Wire, idx indexer.Indexer, holder string, ttl time.Duration) *indexer.Pump {
	return indexer.NewPump(wire, idx, holder, indexer.WithTTL(ttl))
}

// ConnectFromConfig creates a Client based on the transport type in the config.
// Returns an error for HTTP transport since view projections require gRPC services.
func ConnectFromConfig(ctx context.Context, cfg *v1.Config, proj *Projection) (*Client, error) {
	switch cfg.TransportType {
	case v1.TransportTypeGRPC:
		return Connect(ctx, cfg.ServiceURL, proj)
	case v1.TransportTypeMemory:
		sys, err := cfg.SystemFromConfig()
		if err != nil {
			return nil, err
		}
		return ConnectMemory(ctx, sys.Transport, proj)
	case v1.TransportTypeHTTP:
		return nil, fmt.Errorf("view projections require gRPC transport; HTTP transport is not supported")
	default:
		return nil, fmt.Errorf("unsupported transport type: %s", cfg.TransportType)
	}
}

// Get retrieves entity state with optional version constraints.
func (c *Client) Get(ctx context.Context, kind string, key Key, opts ...GetOption) (*Entity, *Result, error) {
	entity, err := c.store.Get(ctx, kind, key)
	if err != nil {
		return nil, nil, err
	}

	result := &Result{
		Entity: entity,
		Status: StatusOK,
	}

	cfg := &getOptions{}
	for _, opt := range opts {
		opt.applyGet(cfg)
	}

	if cfg.untilVersion != nil {
		if entity != nil && entity.Version >= *cfg.untilVersion {
			return entity, result, nil
		}
		return c.waitForVersion(ctx, kind, key, *cfg.untilVersion, cfg.untilTimeout, entity, result)
	}

	if entity == nil {
		result.Status = StatusNotFound
		return nil, result, nil
	}

	result.Status = c.applyAfterConstraint(entity, cfg)
	return entity, result, nil
}

func (c *Client) applyAfterConstraint(entity *Entity, cfg *getOptions) Status {
	if cfg.afterVersion != nil && entity.Version < *cfg.afterVersion {
		return StatusStale
	}
	return StatusOK
}

func (c *Client) waitForVersion(ctx context.Context, kind string, key Key, targetVersion int64, timeout time.Duration, entity *Entity, result *Result) (*Entity, *Result, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	ch := make(chan Change, 16)
	unsub := c.notifier.OnChange(func(chg Change) {
		select {
		case ch <- chg:
		default:
		}
	})
	defer unsub()

	if c.storeAlreadyAtVersion(ctx, targetVersion) {
		return c.refetchEntity(ctx, kind, key, result)
	}

	for {
		select {
		case <-timer.C:
			result.Status = StatusTimeout
			return entity, result, nil
		case chg := <-ch:
			if chg.Version >= targetVersion {
				return c.refetchEntity(ctx, kind, key, result)
			}
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
}

func (c *Client) refetchEntity(ctx context.Context, kind string, key Key, result *Result) (*Entity, *Result, error) {
	entity, err := c.store.Get(ctx, kind, key)
	if err != nil {
		return nil, nil, err
	}
	if entity != nil {
		result.Entity = entity
		result.Status = StatusOK
	} else {
		result.Status = StatusNotFound
	}
	return entity, result, nil
}

// storeAlreadyAtVersion checks whether the projection store has already reached
// the target version, avoiding the race where the pump finished processing before
// the Get subscriber registered with the notifier.
//
// Requires targetVersion > 0 because the store's version defaults to 0 (no events
// processed), which is indistinguishable from having processed the first event when
// event IDs start at 0 (memory transport).
func (c *Client) storeAlreadyAtVersion(ctx context.Context, targetVersion int64) bool {
	if targetVersion <= 0 {
		return false
	}
	v, err := c.Version(ctx)
	return err == nil && v >= targetVersion
}

// Version returns the current projection version.
func (c *Client) Version(ctx context.Context) (int64, error) {
	if memStore, ok := c.store.(*MemoryStore); ok {
		return memStore.Version(), nil
	}
	if c.consumer != nil {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		resp, err := c.consumer.GetVersion(ctx, &vgrpc.GetVersionRequest{
			Projection: c.projection.consumerName,
			Domain:     c.projection.domain,
			Stream:     c.projection.stream,
		})
		if err != nil {
			return 0, fmt.Errorf("get version: %w", err)
		}
		return resp.Version, nil
	}
	return 0, fmt.Errorf("Version not supported: no consumer client available")
}

// OnChange registers a callback for change notifications.
// Returns an unsubscribe function that removes the callback when called.
func (c *Client) OnChange(fn func(Change)) func() {
	return c.notifier.OnChange(fn)
}

// Close stops the Pump and releases resources.
func (c *Client) Close() error {
	if c.cancel != nil {
		c.cancel()
	}
	return nil
}

// LastError returns the last error from the pump, or nil if no error has occurred.
// This is useful for checking if the pump has failed.
func (c *Client) LastError() error {
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
func (c *Client) Errors() <-chan error {
	return c.errChan
}
