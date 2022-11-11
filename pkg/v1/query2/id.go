package query2

import (
	"context"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
)

type IDClause struct {
	id int64
	on v1.OnStreamQueryResult
}

func (i *IDClause) On(process v1.OnStreamQueryResult) {
	i.on = process
}

func (i *IDClause) prepareRequest(ctx context.Context, r *v1.WireBatchR2Request, registry *handlers) error {
	r.OnID = append(r.OnID, v1.WireBatchR2IDQuery{
		Op: registry.register(i.on),
		ID: i.id,
	})
	return nil
}
