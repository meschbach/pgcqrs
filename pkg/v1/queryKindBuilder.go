package v1

import "encoding/json"

type KindBuilder struct {
	kind  string
	eq    []equalityPredicate
	match json.RawMessage
	on    OnStreamQueryResult
}

func (k *KindBuilder) Match(example interface{}) *KindBuilder {
	serialized, err := json.Marshal(example)
	if err != nil {
		panic(err)
	}
	k.match = serialized
	return k
}

func (k *KindBuilder) MatchDocument(serialized string) *KindBuilder {
	k.match = json.RawMessage(serialized)
	return k
}

// On registers handler to be invoked when streaming results.  If invoked multiple times the last invocation will be called.
func (k *KindBuilder) On(handler OnStreamQueryResult) *KindBuilder {
	k.on = handler
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

func (k *KindBuilder) toKindConstraint() KindConstraint {
	var matchers []WireMatcherV1
	for _, m := range k.eq {
		matchers = append(matchers, WireMatcherV1{
			Property: m.Property,
			Value:    []string{m.Value},
		})
	}
	return KindConstraint{
		Kind:        k.kind,
		Eq:          matchers,
		MatchSubset: k.match,
	}
}

func (k *KindBuilder) postProcessing(p *postProcessingHandlers) {
	if k.on != nil {
		p.register(k.kind, k.on)
	}
}
