package v1

import (
	"context"
	"encoding/json"
	"github.com/meschbach/pgcqrs/pkg/ipc"
)

type Transport interface {
	EnsureStream(ctx context.Context, domain string, stream string) error
	Submit(ctx context.Context, domain, stream, kind string, event interface{}) (*Submitted, error)
	GetEvent(ctx context.Context, domain, stream string, id int64, event interface{}) error
	AllEnvelopes(ctx context.Context, domain, stream string) ([]Envelope, error)

	// Query performs a query against the specified domain and stream, using the provided WireQuery for constraints and
	// outputs the result.  This is a historic call site conforming to v1 query semantics.
	Query(ctx context.Context, domain, stream string, query WireQuery, out *WireQueryResult) error
	QueryBatch(ctx context.Context, domain, stream string, query WireQuery, out *WireBatchResults) error
	QueryBatchR2(ctx context.Context, domain, stream string, batch *WireBatchR2Request, out *WireBatchR2Result) error
	Meta(ctx context.Context) (WireMetaV1, error)
	Watch(ctx context.Context, query ipc.QueryIn) (<-chan ipc.QueryOut, error)
}

type StreamTransport interface {
	QueryBatchR2(ctx context.Context, batch *WireBatchR2Request) (*WireBatchR2Result, error)
	//todo: relocate
	Watch(ctx context.Context, query ipc.QueryIn) (<-chan ipc.QueryOut, error)
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

type KindConstraint struct {
	Kind string          `json:"kind"`
	Eq   []WireMatcherV1 `json:"$eq,omitempty"`
	//MatchSubset is the JSON structure we must match in order to return the target kind
	MatchSubset json.RawMessage `json:"$sub,omitempty"`
}

type WireMatcherV1 struct {
	//Property represents a path to the JSON property to be tested.
	Property []string `json:"key"`
	//Value represents acceptable values.  At this time only a single value is supported.
	Value []string `json:"in"`
}

type WireMetaV1 struct {
	Domains []WireMetaDomainV1 `json:"domains"`
}

type WireMetaDomainV1 struct {
	Name    string   `json:"name"`
	Streams []string `json:"streams"`
}
