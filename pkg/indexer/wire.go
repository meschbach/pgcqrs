package indexer

import (
	"context"
	"time"
)

// Wire connects the Pump to pgcqrs for lock acquisition.
// The generic type parameter L allows different transports to return their specific lock types.
type Wire[L Lock] interface {
	WaitForLock(ctx context.Context, domain, stream, consumer, holder string, ttl time.Duration) (L, int64, time.Duration, error)
}
