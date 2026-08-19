package views

import "context"

// Store handles entity storage and retrieval for view projections.
// It is bound to a single projection at construction time.
//
// Note: PGStore does not implement this interface. See PGStore documentation for details.
type Store interface {
	// Get retrieves an entity by kind and key. Returns nil entity with no error if not found.
	Get(ctx context.Context, kind string, key Key) (*Entity, error)
	// Persist applies mutations atomically and returns a Change describing what was written.
	Persist(ctx context.Context, result *ReduceResult, eventID int64) (*Change, error)
}
