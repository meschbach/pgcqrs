package v1

import (
	"context"
	"github.com/meschbach/pgcqrs/internal/junk"
)

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

func NewSystem(storage Transport) *System {
	return &System{Transport: storage}
}
