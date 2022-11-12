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

// Stream obtains the event stream for a given domain (app) and stream.  If the domain + stream does not exist yet then
// one is created.
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

func (s *System) ListDomains(ctx context.Context) ([]string, error) {
	meta, err := s.Transport.Meta(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, d := range meta.Domains {
		names = append(names, d.Name)
	}
	return names, nil
}

type DomainStreamPair struct {
	Domain string
	App    string
}

func (s *System) ListStreams(ctx context.Context) ([]DomainStreamPair, error) {
	meta, err := s.Transport.Meta(ctx)
	if err != nil {
		return nil, err
	}
	var names []DomainStreamPair
	for _, d := range meta.Domains {
		for _, s := range d.Streams {
			names = append(names, DomainStreamPair{
				Domain: d.Name,
				App:    s,
			})
		}
	}
	return names, nil
}

func NewSystem(storage Transport) *System {
	return &System{Transport: storage}
}
