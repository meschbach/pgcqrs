package views

import (
	"context"
	"encoding/json"
	"fmt"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
)

// ProjectionIdentity uniquely identifies a view projection within a stream.
// It carries the tightly coupled {domain, stream, projection} triple.
type ProjectionIdentity struct {
	Domain     string
	Stream     string
	Projection string
}

// NewProjectionIdentity creates a ProjectionIdentity from its component parts.
func NewProjectionIdentity(domain, stream, projection string) ProjectionIdentity {
	return ProjectionIdentity{
		Domain:     domain,
		Stream:     stream,
		Projection: projection,
	}
}

// ProjectionOption configures a Projection.
type ProjectionOption interface {
	applyProjection(*Projection)
}

// ConsumerName overrides the default projection-name-as-consumer-name.
func ConsumerName(name string) ProjectionOption {
	return consumerNameOption{name: name}
}

type consumerNameOption struct {
	name string
}

func (o consumerNameOption) applyProjection(p *Projection) {
	p.consumerName = o.name
}

// Projection defines a materialized view with typed per-kind handlers.
type Projection struct {
	domain       string
	stream       string
	consumerName string
	handlers     map[string]rawHandler
}

// rawHandler is a function that unmarshals raw JSON into a typed event,
// calls the user's handler, and returns the ReduceResult.
type rawHandler func(ctx context.Context, e v1.Envelope, rawJSON json.RawMessage, actx *ReduceContext) (*ReduceResult, error)

// NewProjection creates a new Projection for the given domain and stream.
// The projection name defaults to the consumer name for position tracking.
func NewProjection(domain, stream string, opts ...ProjectionOption) *Projection {
	p := &Projection{
		domain:       domain,
		stream:       stream,
		consumerName: domain,
		handlers:     make(map[string]rawHandler),
	}
	for _, opt := range opts {
		opt.applyProjection(p)
	}
	return p
}

// OnKind registers a typed handler for the given event kind.
// The handler receives the typed event struct (automatically unmarshaled from JSON)
// and a ReduceContext for previous-state retrieval.
func OnKind[T any](kind string, handler func(ctx context.Context, e v1.Envelope, evt *T, actx *ReduceContext) (*ReduceResult, error)) ProjectionOption {
	return onKindOption[T]{
		kind:    kind,
		handler: handler,
	}
}

type onKindOption[T any] struct {
	kind    string
	handler func(ctx context.Context, e v1.Envelope, evt *T, actx *ReduceContext) (*ReduceResult, error)
}

func (o onKindOption[T]) applyProjection(p *Projection) {
	h := o.handler
	p.handlers[o.kind] = func(ctx context.Context, e v1.Envelope, rawJSON json.RawMessage, actx *ReduceContext) (*ReduceResult, error) {
		var evt T
		if err := json.Unmarshal(rawJSON, &evt); err != nil {
			return nil, fmt.Errorf("unmarshal event %s: %w", o.kind, err)
		}
		return h(ctx, e, &evt, actx)
	}
}
