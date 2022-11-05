package v1

import (
	"context"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Query constructs a new query builder for targeting the requested resource
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

// QueryBuilder accumulates and prepares a request to obtain the matching events
type QueryBuilder struct {
	//stream is the target stream to make th request on
	stream *Stream
	//kinds maps the text name of the kind and the constraints for the matching events
	kinds map[string]*KindBuilder
}

// WithKind matches all events with kind with optionally additional constraint for matching.  All constraints are
// `and` operations.  There is no `or` predicates.
func (q *QueryBuilder) WithKind(kind string) *KindBuilder {
	if _, has := q.kinds[kind]; !has {
		q.kinds[kind] = &KindBuilder{kind: kind}
	}
	return q.kinds[kind]
}

// Perform executes the query against the remote PGCQRS system retrieving just envelope information
func (q *QueryBuilder) Perform(ctx context.Context) (QueryResults, error) {
	handler := newHandlers()
	features := &requiredFeatures{}
	query := WireQuery{KindConstraint: nil}
	for _, v := range q.kinds {
		query.KindConstraint = append(query.KindConstraint, v.toKindConstraint(handler, features))
	}
	handler = nil

	result, err := q.stream.performQuery(ctx, query)
	if err != nil {
		return nil, err
	}

	//Ensure results are properly filtered
	if !result.Filtered || !result.SubsetMatch {
		span := trace.SpanFromContext(ctx)
		span.AddEvent("local-processing", trace.WithAttributes(attribute.Bool("filtered", result.Filtered), attribute.Bool("subset-match", result.SubsetMatch)))
		var matching []Envelope
		for _, e := range result.Matching {
			matched, err := filter(ctx, q.stream, query, e)
			if err != nil {
				return nil, err
			}
			if matched {
				matching = append(matching, e)
			}
		}
		result.Filtered = true
		result.SubsetMatch = true
		result.Matching = matching
	}

	return &wireResultInterpreter{results: result}, err
}

type equalityPredicate struct {
	Property []string `json:"path"`
	Value    string   `json:"value"`
}

// QueryResults is a result set with just a set of envelopes.  Really should be titled EnvelopeResults in future API
// revisions but is not since this was the first crack at building the system.
type QueryResults interface {
	//Envelopes returns an array of all envelopes which matched all entities
	Envelopes() []Envelope
}

type wireResultInterpreter struct {
	results WireQueryResult
}

func (w *wireResultInterpreter) Envelopes() []Envelope {
	return w.results.Matching
}
