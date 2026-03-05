package v1

import (
	"context"

	"github.com/meschbach/pgcqrs/pkg/ipc"
)

// WatchInternal is the internal interface for watching a stream.
type WatchInternal interface {
	Tick(ctx context.Context) (message *ipc.QueryOut, err error)
}
