package views

import (
	"context"
	"errors"
)

// MemoryReader is a Reader backed by an in-memory store and the local notifier.
// It is used by With when the system's transport is an in-memory transport.
type MemoryReader struct {
	store    *MemoryStore
	notifier *Notifier
}

// NewMemoryReader creates a Reader backed by the given in-memory store and notifier.
func NewMemoryReader(store *MemoryStore, notifier *Notifier) *MemoryReader {
	return &MemoryReader{store: store, notifier: notifier}
}

// Get retrieves an entity by kind and key, resolving version constraints against
// the in-memory store and the local notifier.
func (r *MemoryReader) Get(ctx context.Context, kind string, key Key, opts ...GetOption) (*Entity, Status, error) {
	cfg := &getOptions{}
	for _, opt := range opts {
		opt.applyGet(cfg)
	}

	entity, err := r.store.Get(ctx, kind, key)
	if err != nil {
		return nil, 0, err
	}

	if cfg.untilVersion != nil {
		return r.waitUntilVersion(ctx, kind, key, entity, cfg)
	}

	if cfg.afterVersion != nil {
		if entity == nil {
			return nil, StatusNotFound, nil
		}
		if entity.Version < *cfg.afterVersion {
			return entity, StatusStale, nil
		}
		return entity, StatusOK, nil
	}

	if entity == nil {
		return nil, StatusNotFound, nil
	}
	return entity, StatusOK, nil
}

func (r *MemoryReader) waitUntilVersion(ctx context.Context, kind string, key Key, entity *Entity, cfg *getOptions) (*Entity, Status, error) {
	if entity != nil && entity.Version >= *cfg.untilVersion {
		return entity, StatusOK, nil
	}

	waitingContext, cancel := context.WithTimeout(ctx, cfg.untilTimeout)
	defer cancel()
	reached, err := r.notifier.WaitForVersion(waitingContext, *cfg.untilVersion)
	if !reached {
		return r.timeoutResult(ctx, kind, key, err)
	}
	return r.refetch(ctx, kind, key)
}

// timeoutResult resolves the case where the wait ended without reaching the target
// version. A wait that only exhausted its own deadline returns the current entity
// with StatusTimeout; any other end maps to the wait error.
func (r *MemoryReader) timeoutResult(ctx context.Context, kind string, key Key, waitErr error) (*Entity, Status, error) {
	if !errors.Is(waitErr, context.DeadlineExceeded) || ctx.Err() != nil {
		return nil, 0, waitErr
	}
	current, err := r.store.Get(ctx, kind, key)
	if err != nil {
		return nil, 0, err
	}
	return current, StatusTimeout, nil
}

// refetch reads the current entity after the target version was reached,
// classifying a nil entity as StatusNotFound.
func (r *MemoryReader) refetch(ctx context.Context, kind string, key Key) (*Entity, Status, error) {
	current, err := r.store.Get(ctx, kind, key)
	if err != nil {
		return nil, 0, err
	}
	if current == nil {
		return nil, StatusNotFound, nil
	}
	return current, StatusOK, nil
}

// Version returns the last processed event ID.
func (r *MemoryReader) Version(_ context.Context) (int64, error) {
	return r.store.Version(), nil
}
