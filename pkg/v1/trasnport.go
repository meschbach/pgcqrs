package v1

import "context"

type Transport interface {
	EnsureStream(ctx context.Context, domain string, stream string) error
	Submit(ctx context.Context, domain, stream, kind string, event interface{}) (*Submitted, error)
	GetEvent(ctx context.Context, domain, stream string, id int64, event interface{}) error
	AllEnvelopes(ctx context.Context, domain, stream string) ([]Envelope, error)
	Query(ctx context.Context, domain, stream string, query WireQuery, out *WireQueryResult) error
}

type WireQuery struct {
	KindConstraint []KindConstraint
}

type WireQueryResult struct {
	//Filtered indicates the result has been filtered according to the specified matchers in the query.  If not then the
	//client is responsible for filtering.
	Filtered bool `json:"filtered"`
	Matching []Envelope
}

type KindConstraint struct {
	Kind string          `json:"kind"`
	Eq   []WireMatcherV1 `json:"$eq"`
}

type WireMatcherV1 struct {
	//Property represents a path to the JSON property to be tested.
	Property []string `json:"key"`
	//Value represents acceptable values.  At this time only a single value is supported.
	Value []string `json:"in"`
}
