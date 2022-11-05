package v1

import (
	"context"
	"encoding/json"
)

type Transport interface {
	EnsureStream(ctx context.Context, domain string, stream string) error
	Submit(ctx context.Context, domain, stream, kind string, event interface{}) (*Submitted, error)
	GetEvent(ctx context.Context, domain, stream string, id int64, event interface{}) error
	AllEnvelopes(ctx context.Context, domain, stream string) ([]Envelope, error)
	Query(ctx context.Context, domain, stream string, query WireQuery, out *WireQueryResult) error
	QueryBatch(ctx context.Context, domain, stream string, query WireQuery, out *WireBatchResults) error
}

type WireQuery struct {
	KindConstraint []KindConstraint
}

type WireQueryResult struct {
	//Filtered indicates the result has been filtered according to the specified matchers in the query.  If not then the
	//client is responsible for filtering.
	Filtered    bool `json:"filtered"`
	SubsetMatch bool `json:"subsetMatch,omitempty"`
	Matching    []Envelope
}

type DisjointMatch struct {
	Match json.RawMessage `json:"$eq"`
	ID    int             `json:"id"`
}

type KindConstraint struct {
	Kind string          `json:"kind"`
	Eq   []WireMatcherV1 `json:"$eq,omitempty"`
	//MatchSubset is the JSON structure we must match in order to return the target kind
	MatchSubset json.RawMessage `json:"$sub,omitempty"`
	//Disjoint are alternate matches of a given document
	Disjoint []DisjointMatch `json:"$or,omitempty"`
}

type WireMatcherV1 struct {
	//Property represents a path to the JSON property to be tested.
	Property []string `json:"key"`
	//Value represents acceptable values.  At this time only a single value is supported.
	Value []string `json:"in"`
}
