package query2

import (
	"context"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
)

// IDClause allows for querying by a specific ID.
type IDClause struct {
	id int64
	on v1.OnStreamQueryResult
}

// On specifies the handler for the query results.
func (i *IDClause) On(process v1.OnStreamQueryResult) {
	i.on = process
}

func (i *IDClause) prepareRequest(_ context.Context, r *v1.WireBatchR2Request, registry *handlers) error {
	r.OnID = append(r.OnID, v1.WireBatchR2IDQuery{
		Op: registry.register(i.on),
		ID: i.id,
	})
	return nil
}
