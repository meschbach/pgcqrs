package indexer

import (
	"context"
	"time"

	"github.com/meschbach/pgcqrs/pkg/ipc"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
)

// Wire connects the Pump to pgcqrs for events, locks, and positions.
type Wire interface {
	Watch(ctx context.Context, query *ipc.QueryIn) (v1.WatchInternal, error)
	TryAcquire(ctx context.Context, domain, stream, consumer, holder string, ttl time.Duration) (*v1.LockResult, error)
	NewKeepAlive(ctx context.Context, domain, stream, consumer, holder string) (Lock, error)
	GetPosition(ctx context.Context, domain, stream, consumer string) (int64, bool, error)
}
