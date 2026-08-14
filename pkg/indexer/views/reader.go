package views

import "context"

// Reader is the application read path for a view projection. It is bound to a
// single projection at construction time and supports version-constrained reads.
type Reader interface {
	// Get retrieves an entity by kind and key, resolving version constraints.
	// Returns a nil entity with StatusNotFound when the entity does not exist.
	Get(ctx context.Context, kind string, key Key, opts ...GetOption) (*Entity, Status, error)
	// Version returns the current projection version.
	Version(ctx context.Context) (int64, error)
}
