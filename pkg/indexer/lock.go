package indexer

import "context"

// Lock provides heartbeating and release for consumer locks.
type Lock interface {
	Heartbeat(ctx context.Context, position int64) error
	Release(ctx context.Context) error
}
