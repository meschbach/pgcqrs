package indexer

import (
	"context"
	"time"

	"github.com/meschbach/pgcqrs/pkg/ipc"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
)

// Wire connects the Pump to pgcqrs for events and locks.
// The generic type parameter L allows different transports to return their specific lock types.
type Wire[L Lock] interface {
	Watch(ctx context.Context, query *ipc.QueryIn) (v1.WatchInternal, error)
	WaitForLock(ctx context.Context, domain, stream, consumer, holder string, ttl time.Duration) (L, int64, time.Duration, error)
}
