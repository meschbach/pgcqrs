package v1

import (
	"context"
	"encoding/json"

	"github.com/meschbach/pgcqrs/pkg/ipc"
	"github.com/meschbach/pgcqrs/pkg/v1/local"
)

type queryInFilter struct {
	core  *memory
	query *ipc.QueryIn
}

func (q *queryInFilter) filter(ctx context.Context, e Envelope, send func(int64, Envelope, json.RawMessage)) error {
	for _, onKind := range q.query.OnKind {
		if onKind.Kind != e.Kind {
			continue
		}
		if err := q.processOnKind(ctx, onKind, e, send); err != nil {
			return err
		}
	}
	return nil
}

func (q *queryInFilter) processOnKind(ctx context.Context, onKind *ipc.OnKindClause, e Envelope, send func(int64, Envelope, json.RawMessage)) error {
	if onKind.AllOp != nil {
		var data json.RawMessage
		if err := q.core.GetEvent(ctx, q.query.Events.Domain, q.query.Events.Stream, e.ID, &data); err != nil {
			return err
		}
		send(*onKind.AllOp, e, data)
	}

	for _, match := range onKind.Subsets {
		if err := q.processMatch(ctx, match, e, send); err != nil {
			return err
		}
	}
	return nil
}

func (q *queryInFilter) processMatch(ctx context.Context, match *ipc.OnKindSubsetMatch, e Envelope, send func(int64, Envelope, json.RawMessage)) error {
	var data json.RawMessage
	if err := q.core.GetEvent(ctx, q.query.Events.Domain, q.query.Events.Stream, e.ID, &data); err != nil {
		return err
	}
	if local.JSONIsSubset(data, match.Match) {
		send(match.Op, e, data)
	}
	return nil
}
