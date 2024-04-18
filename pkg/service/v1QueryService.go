package service

import (
	"context"
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
	var body []byte
	err := q.transport.GetEvent(ctx, in.Events.Domain, in.Events.Stream, in.Id, &body)
	if err != nil {
		return nil, err
	}
	out := &ipc.GetOut{
		Payload: body,
	}
	return out, nil
}

func (q *V1QueryService) Query(input *ipc.QueryIn, output ipc.Query_QueryServer) error {
	query := v1.WireBatchR2Request{}
	mappedIDs := make(map[int]int64)
	nextID := 0
	for _, onKind := range input.OnKind {
		id := nextID
		mappedIDs[id] = *onKind.AllOp

		query.OnKinds = append(query.OnKinds, v1.WireBatchR2KindQuery{
			Kind: onKind.Kind,
			All:  &id,
		})
	}
	out := v1.WireBatchR2Result{}
	err := q.transport.QueryBatchR2(output.Context(), input.Events.Domain, input.Events.Stream, &query, &out)
	if err != nil {
		return err
	}
	for _, result := range out.Results {
		op := mappedIDs[result.Op]
		msg := &ipc.QueryOut{
			Op: op,
			Id: &result.Envelope.ID,
			Envelope: &ipc.MaterializedEnvelope{
				Id:   result.Envelope.ID,
				Kind: result.Envelope.Kind,
			},
			Body: result.Event,
		}
		if err := output.Send(msg); err != nil {
			return err
		}
	}
	return nil
}
