package service

import (
	"context"
	"github.com/meschbach/pgcqrs/pkg/ipc"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func ProxyingCommandService(transport v1.Transport) *V1CommandService {
	return &V1CommandService{
		transport: transport,
	}
}

// V1CommandService wraps a v1 Transport mechanisms, effectively operating as a proxy to the service
type V1CommandService struct {
	ipc.UnimplementedCommandServer
	transport v1.Transport
}

func (c *V1CommandService) CreateStream(ctx context.Context, in *ipc.CreateStreamIn) (*ipc.CreateStreamOut, error) {
	if in == nil || in.Target == nil {
		return nil, status.Error(codes.InvalidArgument, "nil")
	}
	//todo: verify this does not exist yet
	err := c.transport.EnsureStream(ctx, in.Target.Domain, in.Target.Stream)
	if err != nil {
		return nil, err
	}
	out := &ipc.CreateStreamOut{Existed: false}
	return out, err
}

func (c *V1CommandService) Submit(ctx context.Context, in *ipc.SubmitIn) (*ipc.SubmitOut, error) {
	result, err := c.transport.Submit(ctx, in.Events.Domain, in.Events.Stream, in.Kind, in.Body)
	if err != nil {
		return nil, err
	}
	out := &ipc.SubmitOut{
		Id:    result.ID,
		State: &ipc.Consistency{After: result.ID},
	}
	return out, nil
}
