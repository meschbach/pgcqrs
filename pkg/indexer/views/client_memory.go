package views

import (
	"context"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
)

// newMemoryClient builds a projection client backed by an in-memory store.
func newMemoryClient(ctx context.Context, transport v1.Transport, proj *Projection, opts []ClientOption) (ProjectionClient, error) {
	store := NewMemoryStore()
	parts := clientParts[*v1.MemoryLock]{
		wire:  v1.NewMemoryWire(transport),
		store: store,
		buildReader: func(n *Notifier) Reader {
			return NewMemoryReader(store, n)
		},
	}
	return newClient(ctx, transport, proj, parts, opts)
}
