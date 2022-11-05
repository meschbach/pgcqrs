package v1

import "encoding/json"

type KindBuilder struct {
	kind      string
	eq        []equalityPredicate
	disjoints []*kindMatchResult
	current   *kindMatchResult
}

type kindMatchResult struct {
	match json.RawMessage
	on    OnStreamQueryResult
}

func (k *KindBuilder) Match(example interface{}) *KindBuilder {
	serialized, err := json.Marshal(example)
	if err != nil {
		panic(err)
	}
	k.current = &kindMatchResult{
		match: serialized,
		on:    nil,
	}
	k.disjoints = append(k.disjoints, k.current)
	return k
}

// On registers handler to be invoked when streaming results.  If invoked multiple times the last invocation will be called.
func (k *KindBuilder) On(handler OnStreamQueryResult) *KindBuilder {
	k.current.on = handler
	return k
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

func (k *KindBuilder) toKindConstraint(p *postProcessingHandlers, requiredFeatures *requiredFeatures) KindConstraint {
	var matchers []WireMatcherV1
	for _, m := range k.eq {
		matchers = append(matchers, WireMatcherV1{
			Property: m.Property,
			Value:    []string{m.Value},
		})
	}

	constraint := KindConstraint{
		Kind: k.kind,
		Eq:   matchers,
	}

	disjointCount := len(k.disjoints)
	if disjointCount == 1 {
		p.register(k.kind, k.current.on)
		constraint.MatchSubset = k.current.match
	} else if disjointCount > 1 {
		requiredFeatures.disjoints()
		out := make([]DisjointMatch, disjointCount)
		for i, d := range k.disjoints {
			id := p.registerSubhandler(k.kind, d.on)
			out[i] = DisjointMatch{Match: d.match, ID: id}
		}
		constraint.Disjoint = out
	}
	return constraint
}
