package v1

import (
	"context"
	"github.com/meschbach/go-junk-bucket/pkg/fx"
	"github.com/meschbach/pgcqrs/internal/junk"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/slices"
)

type Transport interface {
	EnsureStream(ctx context.Context, domain string, stream string) error
	Submit(ctx context.Context, domain, stream, kind string, event interface{}) (*Submitted, error)
	GetEvent(ctx context.Context, domain, stream string, id int64, event interface{}) error
	AllEnvelopes(ctx context.Context, domain, stream string) ([]Envelope, error)
}

type System struct {
	Transport Transport
}

func (s *System) MustStream(ctx context.Context, domain, stream string) *Stream {
	out, err := s.Stream(ctx, domain, stream)
	junk.Must(err)
	return out
}

func (s *System) Stream(ctx context.Context, domain string, stream string) (*Stream, error) {
	if err := s.Transport.EnsureStream(ctx, domain, stream); err != nil {
		return nil, err
	}
	return &Stream{
		system: s,
		domain: domain,
		stream: stream,
	}, nil
}

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

	envelopes, err := s.All(ctx)
	if err != nil {
		return nil, err
	}
	out := fx.Filter[Envelope](envelopes, func(e Envelope) bool {
		return slices.Contains(kinds, e.Kind)
	})
	return out, nil
}

func (s *Stream) MustByKind(ctx context.Context, kinds ...string) []Envelope {
	out, err := s.ByKinds(ctx, kinds...)
	junk.Must(err)
	return out
}

func NewSystem(storage Transport) *System {
	return &System{Transport: storage}
}
