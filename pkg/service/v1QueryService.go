package service

import (
	"context"
	"encoding/json"
	"github.com/meschbach/pgcqrs/pkg/ipc"
	"github.com/meschbach/pgcqrs/pkg/v1"
)

func ProxyQueryService(transport v1.Transport) *V1QueryService {
	return &V1QueryService{
		transport: transport,
	}
}

// V1QueryService wraps a v1 Transport mechanism gateway for a query service.
type V1QueryService struct {
	ipc.UnimplementedQueryServer
	transport v1.Transport
}

func (q *V1QueryService) Get(ctx context.Context, in *ipc.GetIn) (*ipc.GetOut, error) {
	var body json.RawMessage
	err := q.transport.GetEvent(ctx, in.Events.Domain, in.Events.Stream, in.Id, &body)
	if err != nil {
		return nil, err
	}
	out := &ipc.GetOut{
		Payload: body,
	}
	return out, nil
}
