package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/meschbach/pgcqrs/pkg/ipc"
	"github.com/meschbach/pgcqrs/pkg/v1/local"
)

type queryInFilter struct {
	core  *memory
	query *ipc.QueryIn
}

func (q *queryInFilter) filter(ctx context.Context, e Envelope, send func(int64, Envelope, json.RawMessage)) error {
	for _, onKind := range q.query.OnKind {
		if onKind.Kind == e.Kind {
			if onKind.AllOp != nil {
				var data json.RawMessage
				if err := q.core.GetEvent(ctx, q.query.Events.Domain, q.query.Events.Stream, e.ID, &data); err != nil {
					return err
				}
				send(*onKind.AllOp, e, data)
			}
			for _, match := range onKind.Subsets {
				fmt.Printf("\t\tmatching %q\n", match.Match)
				var data json.RawMessage
				if err := q.core.GetEvent(ctx, q.query.Events.Domain, q.query.Events.Stream, e.ID, &data); err != nil {
					fmt.Printf("\t\tfailed to get event: %v\n", err)
					return err
				}
				fmt.Printf("\t\tmatching event: %q\n", data)
				if local.JSONIsSubset(data, match.Match) {
					fmt.Printf("\t\tmatched: %q v %q\n", match.Match, data)
					send(match.Op, e, data)
				} else {
					fmt.Printf("\t\tdid not match: %q v %q\n", match.Match, data)
				}
			}
		}
	}
	return nil
}
