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
	Matching []Envelope
}

type KindConstraint struct {
	Kind string
}
