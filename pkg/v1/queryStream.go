package v1

import (
	"context"
	"encoding/json"
	"errors"
)

type postProcessingHandlers struct {
	typedHandlers map[string]OnStreamQueryResult
	subhandlers   map[string][]OnStreamQueryResult
}

func newHandlers() *postProcessingHandlers {
	return &postProcessingHandlers{
		typedHandlers: make(map[string]OnStreamQueryResult),
		subhandlers:   make(map[string][]OnStreamQueryResult),
	}
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

func (p *postProcessingHandlers) registerSubhandler(kind string, result OnStreamQueryResult) int {
	s := p.subhandlers[kind]
	id := len(s)
	s = append(s, result)
	p.subhandlers[kind] = s
	return id
}

type requiredFeatures struct {
	flagDisjoints bool
}

func (r *requiredFeatures) disjoints() {
	r.flagDisjoints = true
}

func (r *requiredFeatures) verifyBatch(batch *WireBatchResults) error {
	if r.flagDisjoints {
		if batch.Features == nil || !batch.Features.Disjoints {
			return errors.New("disjoints required but not present")
		}
	}
	return nil
}

func (q *QueryBuilder) Stream(parentContext context.Context) error {
	ctx, span := tracer.Start(parentContext, "pgcqrs.StreamingQuery")
	defer span.End()

	//Produce request entity and setup post-processing
	handlers := newHandlers()
	features := &requiredFeatures{flagDisjoints: false}
	wireQuery := WireQuery{KindConstraint: nil}
	for _, v := range q.kinds {
		wireQuery.KindConstraint = append(wireQuery.KindConstraint, v.toKindConstraint(handlers, features))
	}
	span.AddEvent("wire-entity assembled")

	//Invocation
	batchResult, err := q.stream.performBatchQuery(ctx, wireQuery)
	if err != nil {
		return err
	}
	if err := features.verifyBatch(&batchResult); err != nil {
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

func EntityFunc[T any](apply func(ctx context.Context, e Envelope, entity T)) OnStreamQueryResult {
	return func(ctx context.Context, e Envelope, rawJSON json.RawMessage) error {
		var t T
		if err := json.Unmarshal(rawJSON, &t); err != nil {
			return err
		}
		apply(ctx, e, t)
		return nil
	}
}

func (s *Stream) QueryBatchR2(ctx context.Context, batch *WireBatchR2Request) (*WireBatchR2Result, error) {
	out := &WireBatchR2Result{}
	err := s.system.Transport.QueryBatchR2(ctx, s.domain, s.stream, batch, out)
	if err != nil {
		return nil, err
	}
	return out, nil
}
