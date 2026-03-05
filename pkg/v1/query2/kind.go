package query2

import (
	"context"
	"encoding/json"

	"github.com/meschbach/pgcqrs/pkg/ipc"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
)

// KindClause allows for querying by a specific document kind.
type KindClause struct {
	kind    string
	each    v1.OnStreamQueryResult
	matched []*MatchedKind
}

// Each specifies a handler to be called for every document of this kind.
func (k *KindClause) Each(onEach v1.OnStreamQueryResult) {
	k.each = onEach
}

// Subset specifies a subset of the document to match against.
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

func (k *KindClause) prepareQuery(ctx context.Context, r *ipc.QueryIn, registry *handlers) error {
	wire := &ipc.OnKindClause{
		Kind: k.kind,
	}
	if all := registry.registerOptional(k.each); all != nil {
		allInt64 := int64(*all)
		wire.AllOp = &allInt64
		if wire.AllOp != nil {
			truthy := true
			wire.AllOpConfig = &ipc.ResultInclude{
				Envelope: &truthy,
				Body:     &truthy,
			}
		}
	}

	for _, m := range k.matched {
		if err := m.prepareQuery(ctx, wire, registry); err != nil {
			return err
		}
	}
	r.OnKind = append(r.OnKind, wire)
	return nil
}

// MatchedKind represents a kind that has been filtered by a subset match.
type MatchedKind struct {
	processor v1.OnStreamQueryResult
	subset    interface{}
}

// On specifies the handler for the matched kind.
func (m *MatchedKind) On(handler v1.OnStreamQueryResult) {
	m.processor = handler
}

func (m *MatchedKind) prepareRequest(_ context.Context, r *v1.WireBatchR2KindQuery, registry *handlers) error {
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

func (m *MatchedKind) prepareQuery(_ context.Context, r *ipc.OnKindClause, registry *handlers) error {
	if m.processor == nil {
		return nil
	}
	doc, err := json.Marshal(m.subset)
	if err != nil {
		return err
	}
	r.Subsets = append(r.Subsets, &ipc.OnKindSubsetMatch{
		Match: doc,
		Op:    int64(registry.register(m.processor)),
	})
	return nil
}
