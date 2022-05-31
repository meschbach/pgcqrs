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
		query.KindConstraint = append(query.KindConstraint, KindConstraint{Kind: v.kind})
	}

	result, err := q.stream.performQuery(ctx, query)
	if err != nil {
		return nil, err
	}

	return &wireResultInterpreter{results: result}, err
}

type KindBuilder struct {
	kind string
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
