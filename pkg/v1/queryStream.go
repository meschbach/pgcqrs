package v1

import (
	"context"
	"encoding/json"
)

type postProcessingHandlers struct {
	typedHandlers map[string]OnStreamQueryResult
}

func (p *postProcessingHandlers) handle(ctx context.Context, result WireBatchResultPair) error {
	if handler, ok := p.typedHandlers[result.Meta.Kind]; ok {
		return handler(ctx, result.Meta, result.Data)
	}
	return nil
}

func (p *postProcessingHandlers) register(kind string, result OnStreamQueryResult) {
	p.typedHandlers[kind] = result
}

func (q *QueryBuilder) Stream(parentContext context.Context) error {
	ctx, span := tracer.Start(parentContext, "pgcqrs.StreamingQuery")
	defer span.End()

	//Produce request entity and setup post-processing
	handlers := &postProcessingHandlers{typedHandlers: make(map[string]OnStreamQueryResult)}
	wireQuery := WireQuery{KindConstraint: nil}
	for _, v := range q.kinds {
		wireQuery.KindConstraint = append(wireQuery.KindConstraint, v.toKindConstraint())
		v.postProcessing(handlers)
	}
	span.AddEvent("wire-entity assembled")

	//Invocation
	batchResult, err := q.stream.performBatchQuery(ctx, wireQuery)
	if err != nil {
		return err
	}

	//Dispatch results
	span.AddEvent("dispatching results")
	for _, data := range batchResult.Page {
		if err := handlers.handle(ctx, data); err != nil {
			return err
		}
	}

	return nil
}

type OnStreamQueryResult = func(ctx context.Context, e Envelope, rawJSON json.RawMessage) error

func (s *Stream) performBatchQuery(ctx context.Context, query WireQuery) (WireBatchResults, error) {
	var out WireBatchResults
	err := s.system.Transport.QueryBatch(ctx, s.domain, s.stream, query, &out)
	return out, err
}
