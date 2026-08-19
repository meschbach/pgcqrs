package views

import (
	"context"
	"encoding/json"
	"fmt"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
)

// ProjectionIdentity uniquely identifies a view projection within a stream.
// It carries the tightly coupled {domain, stream, projection} triple plus
// an optional consumer name for position tracking.
type ProjectionIdentity struct {
	Domain       string
	Stream       string
	Projection   string // storage identity (view_projection_names)
	ConsumerName string // position tracking (consumer_positions); defaults to Projection if empty
}

// NewProjectionIdentity creates a ProjectionIdentity from its component parts.
// If consumerName is empty, it defaults to projection.
func NewProjectionIdentity(domain, stream, projection, consumerName string) ProjectionIdentity {
	if consumerName == "" {
		consumerName = projection
	}
	return ProjectionIdentity{
		Domain:       domain,
		Stream:       stream,
		Projection:   projection,
		ConsumerName: consumerName,
	}
}

// ProjectionOption configures a Projection.
type ProjectionOption interface {
	applyProjection(*Projection)
}

// ProjectionName overrides the default projection name (which defaults to domain).
// Use this to create a separate storage namespace for the projection.
func ProjectionName(name string) ProjectionOption {
	return projectionNameOption{name: name}
}

type projectionNameOption struct {
	name string
}

func (o projectionNameOption) applyProjection(p *Projection) {
	p.projectionName = o.name
}

// ConsumerName overrides the default consumer name (which defaults to domain).
// Use this to create a separate position tracking identity for the projection.
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
	domain         string
	stream         string
	projectionName string // storage identity, defaults to domain
	consumerName   string // position tracking, defaults to domain
	handlers       map[string]rawHandler
}

// rawHandler is a function that unmarshals raw JSON into a typed event,
// calls the user's handler, and returns the ReduceResult.
type rawHandler func(ctx context.Context, e v1.Envelope, rawJSON json.RawMessage, actx *ReduceContext) (*ReduceResult, error)

// NewProjection creates a new Projection for the given domain and stream.
// Both projection name and consumer name default to domain. Use ProjectionName
// and ConsumerName options to override them independently.
func NewProjection(domain, stream string, opts ...ProjectionOption) *Projection {
	p := &Projection{
		domain:         domain,
		stream:         stream,
		projectionName: domain,
		consumerName:   domain,
		handlers:       make(map[string]rawHandler),
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
