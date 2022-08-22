package v1

import (
	"context"
	"encoding/json"
	"github.com/elgs/gojq"
	"github.com/meschbach/pgcqrs/pkg/v1/local"
	"strings"
)

type filterLoader interface {
	Get(ctx context.Context, id int64, payload interface{}) error
}

func filter(ctx context.Context, loader filterLoader, query WireQuery, e Envelope) (bool, error) {
	if len(query.KindConstraint) == 0 {
		return true, nil
	}
	for _, c := range query.KindConstraint {
		if e.Kind != c.Kind {
			continue
		}
		if len(c.Eq) > 0 {
			match, err := filterMatchedProperties(ctx, loader, c, e)
			if err != nil {
				return false, err
			}
			if !match {
				return false, nil
			}
		}
		if len(c.MatchSubset) > 0 {
			match, err := filterSubsetMatch(ctx, loader, c, e)
			if err != nil {
				return false, err
			}
			if !match {
				return false, nil
			}
		}
		return true, nil
	}
	return false, nil
}

func filterMatchedProperties(ctx context.Context, loader filterLoader, c KindConstraint, e Envelope) (bool, error) {
	var body json.RawMessage

	if err := loader.Get(ctx, e.ID, &body); err != nil {
		return false, err
	}

	parser, err := gojq.NewStringQuery(string(body))
	if err != nil {
		return false, err
	}

	for _, e := range c.Eq {
		query := strings.Join(e.Property, ".")
		value, err := parser.QueryToString(query)
		if err != nil {
			//TODO: does not exist seems to be the only possible error
			return false, nil
		}
		//TODO: expand to allow multiple values
		if value != e.Value[0] {
			return false, nil
		}
	}
	return true, nil
}

func filterSubsetMatch(ctx context.Context, loader filterLoader, c KindConstraint, e Envelope) (bool, error) {
	var body json.RawMessage

	if err := loader.Get(ctx, e.ID, &body); err != nil {
		return false, err
	}
	matched := local.JSONIsSubset(body, c.MatchSubset)
	return matched, nil
}
