package v1

import (
	"context"
	"github.com/meschbach/pgcqrs/pkg/ipc"
)

type WatchInternal interface {
	Tick(ctx context.Context) (message *ipc.QueryOut, err error)
}
