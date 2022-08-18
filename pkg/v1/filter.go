package v1

import (
	"context"
	"encoding/json"
	"github.com/elgs/gojq"
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
		if len(c.Eq) == 0 {
			return true, nil
		}

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
	return false, nil
}
