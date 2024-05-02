package memory

import (
	"context"
	"errors"
	"github.com/meschbach/pgcqrs/pkg/ipc"
)

type commands struct {
	ipc.UnimplementedCommandServer
	core *core
}

func (c *commands) CreateStream(ctx context.Context, in *ipc.CreateStreamIn) (*ipc.CreateStreamOut, error) {
	domain, _ := c.core.ensureDomain(in.Target.Domain)
	_, streamExisted := domain.ensureStream(in.Target.Stream)
	return &ipc.CreateStreamOut{Existed: streamExisted}, nil
}

func (c *commands) Submit(ctx context.Context, in *ipc.SubmitIn) (*ipc.SubmitOut, error) {
	id, coordinates, err := c.core.coordinate(in.Expectations)
	if err != nil {
		return nil, err
	}

	stream, has := c.core.lookup(in.Events)
	if !has {
		return nil, errors.New("no such domain and stream")
	}
	stream.submit(id, in.Kind, in.Body)
	return &ipc.SubmitOut{
		Id:    id,
		State: coordinates,
	}, nil
}
