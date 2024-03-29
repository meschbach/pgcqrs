package storage

import (
	"context"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
)

func TranslateBatchR2(ctx context.Context, app, stream string, request *v1.WireBatchR2Request) []Operation {
	var output []Operation
	//Dispatch kinds
	for _, kind := range request.OnKinds {
		if kind.All != nil {
			output = append(output, &EachKind{
				App:    app,
				Stream: stream,
				Op:     *kind.All,
				Kind:   kind.Kind,
			})
		}
		for _, match := range kind.Match {
			output = append(output, &MatchSubset{
				App:    app,
				Stream: stream,
				Op:     match.Op,
				Kind:   kind.Kind,
				Subset: match.Subset,
			})
		}
	}

	//Dispatch IDs
	for _, id := range request.OnID {
		output = append(output, &matchID{
			app:    app,
			stream: stream,
			id:     id.ID,
			op:     id.Op,
		})
	}

	return output
}
