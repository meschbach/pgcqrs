package v1

import (
	"context"
)

func (s *Stream) Query() *QueryBuilder {
	q := &QueryBuilder{stream: s}
	q.kinds = make(map[string]*KindBuilder)
	return q
}

func (s *Stream) performQuery(ctx context.Context, query WireQuery) (WireQueryResult, error) {
	var out WireQueryResult
	err := s.system.Transport.Query(ctx, s.domain, s.stream, query, &out)
	return out, err
}

type QueryBuilder struct {
	stream *Stream
	kinds  map[string]*KindBuilder
}

func (q *QueryBuilder) WithKind(kind string) *KindBuilder {
	if _, has := q.kinds[kind]; !has {
		q.kinds[kind] = &KindBuilder{kind: kind}
	}
	return q.kinds[kind]
}

func (q *QueryBuilder) Perform(ctx context.Context) (QueryResults, error) {
	query := WireQuery{KindConstraint: nil}
	for _, v := range q.kinds {
		query.KindConstraint = append(query.KindConstraint, v.toKindConstraint())
	}

	result, err := q.stream.performQuery(ctx, query)
	if err != nil {
		return nil, err
	}

	//Ensure results are properly filtered
	if !result.Filtered {
		var matching []Envelope
		for _, e := range result.Matching {
			matched, err := filter(ctx, nil, query, e)
			if err != nil {
				return nil, err
			}
			if matched {
				matching = append(matching, e)
			}
		}
		result.Filtered = true
		result.Matching = matching
	}

	return &wireResultInterpreter{results: result}, err
}

type KindBuilder struct {
	kind string
	eq   []equalityPredicate
}

func (k *KindBuilder) Eq(property string, value string) *KindBuilder {
	return k.Equals([]string{property}, value)
}

func (k *KindBuilder) Equals(property []string, value string) *KindBuilder {
	k.eq = append(k.eq, equalityPredicate{
		Property: property,
		Value:    value,
	})
	return k
}

func (k *KindBuilder) toKindConstraint() KindConstraint {
	var matchers []WireMatcherV1
	for _, m := range k.eq {
		matchers = append(matchers, WireMatcherV1{
			Property: m.Property,
			Value:    []string{m.Value},
		})
	}
	return KindConstraint{
		Kind: k.kind,
		Eq:   matchers,
	}
}

type equalityPredicate struct {
	Property []string `json:"path"`
	Value    string   `json:"value"`
}

type QueryResults interface {
	Envelopes() []Envelope
}

type wireResultInterpreter struct {
	results WireQueryResult
}

func (w *wireResultInterpreter) Envelopes() []Envelope {
	return w.results.Matching
}
