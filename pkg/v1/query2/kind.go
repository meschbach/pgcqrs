package query2

import (
	"context"
	"encoding/json"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
)

type KindClause struct {
	kind    string
	each    v1.OnStreamQueryResult
	matched []*MatchedKind
}

func (k *KindClause) Each(onEach v1.OnStreamQueryResult) {
	k.each = onEach
}

func (k *KindClause) Subset(doc interface{}) *MatchedKind {
	m := &MatchedKind{
		processor: nil,
		subset:    doc,
	}
	k.matched = append(k.matched, m)
	return m
}

func (k *KindClause) prepareRequest(ctx context.Context, r *v1.WireBatchR2Request, registry *handlers) error {
	wireQuery := &v1.WireBatchR2KindQuery{
		Kind:  k.kind,
		All:   nil,
		Match: nil,
	}
	wireQuery.All = registry.registerOptional(k.each)
	for _, m := range k.matched {
		if err := m.prepareRequest(ctx, wireQuery, registry); err != nil {
			return err
		}
	}
	r.OnKinds = append(r.OnKinds, *wireQuery)
	return nil
}

type MatchedKind struct {
	processor v1.OnStreamQueryResult
	subset    interface{}
}

func (m *MatchedKind) On(handler v1.OnStreamQueryResult) {
	m.processor = handler
}

func (m *MatchedKind) prepareRequest(ctx context.Context, r *v1.WireBatchR2KindQuery, registry *handlers) error {
	if m.processor == nil {
		return nil
	}
	doc, err := json.Marshal(m.subset)
	if err != nil {
		return err
	}
	r.Match = append(r.Match, v1.WireBatchR2KindMatch{
		Op:     registry.register(m.processor),
		Subset: doc,
	})
	return nil
}
