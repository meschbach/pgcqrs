package v1

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/elgs/gojq"
	"github.com/meschbach/pgcqrs/pkg/v1/local"
)

type filterLoader interface {
	Get(ctx context.Context, id int64, payload interface{}) error
}

func filter(ctx context.Context, loader filterLoader, query WireQuery, e Envelope) (bool, error) {
	if len(query.KindConstraint) == 0 {
		return true, nil
	}

	constraint := findMatchingConstraint(e.Kind, query.KindConstraint)
	if constraint == nil {
		return false, nil
	}

	eqMatch, err := applyEqFilter(ctx, loader, constraint, e)
	if err != nil || !eqMatch {
		return eqMatch, err
	}
	return applySubsetFilter(ctx, loader, constraint, e)
}

func applyEqFilter(ctx context.Context, loader filterLoader, constraint *KindConstraint, e Envelope) (bool, error) {
	if !hasEqConstraints(constraint) {
		return true, nil
	}
	return filterMatchedProperties(ctx, loader, *constraint, e)
}

func applySubsetFilter(ctx context.Context, loader filterLoader, constraint *KindConstraint, e Envelope) (bool, error) {
	if !hasSubsetConstraints(constraint) {
		return true, nil
	}
	return filterSubsetMatch(ctx, loader, *constraint, e)
}

func hasEqConstraints(c *KindConstraint) bool {
	return len(c.Eq) > 0
}

func hasSubsetConstraints(c *KindConstraint) bool {
	return len(c.MatchSubset) > 0
}

func findMatchingConstraint(kind string, constraints []KindConstraint) *KindConstraint {
	for _, c := range constraints {
		if kind == c.Kind {
			constraint := c
			return &constraint
		}
	}
	return nil
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
