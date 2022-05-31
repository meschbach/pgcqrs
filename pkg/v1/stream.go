package v1

import (
	"context"
	"github.com/meschbach/pgcqrs/internal/junk"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type Stream struct {
	system *System
	domain string
	stream string
}

func (s *Stream) Submit(ctx context.Context, kind string, event interface{}) (*Submitted, error) {
	return s.system.Transport.Submit(ctx, s.domain, s.stream, kind, event)
}

func (s *Stream) MustSubmit(ctx context.Context, kind string, event interface{}) *Submitted {
	out, err := s.Submit(ctx, kind, event)
	junk.Must(err)
	return out
}

func (s *Stream) All(ctx context.Context) ([]Envelope, error) {
	return s.system.Transport.AllEnvelopes(ctx, s.domain, s.stream)
}

func (s *Stream) MustAll(ctx context.Context) []Envelope {
	out, err := s.All(ctx)
	junk.Must(err)
	return out
}

func (s *Stream) Get(ctx context.Context, id int64, payload interface{}) error {
	return s.system.Transport.GetEvent(ctx, s.domain, s.stream, id, payload)
}

func (s *Stream) MustGet(ctx context.Context, id int64, payload interface{}) {
	junk.Must(s.Get(ctx, id, payload))
}

func (s *Stream) ByKinds(ctx context.Context, kinds ...string) ([]Envelope, error) {
	ctx, span := tracer.Start(ctx, "pgcqrs.ByKinds", trace.WithAttributes(attribute.StringSlice("kinds", kinds)))
	defer span.End()

	builder := s.Query()
	for _, kind := range kinds {
		builder.WithKind(kind)
	}
	result, err := builder.Perform(ctx)
	if err != nil {
		return nil, err
	}

	return result.Envelopes(), nil
}

func (s *Stream) MustByKind(ctx context.Context, kinds ...string) []Envelope {
	out, err := s.ByKinds(ctx, kinds...)
	junk.Must(err)
	return out
}
