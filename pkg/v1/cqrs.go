package v1

import (
	"context"

	"github.com/meschbach/pgcqrs/internal/junk"
)

// System provides a top-level interface for interacting with the CQRS store.
type System struct {
	Transport Transport
}

// MustStream ensures the given domain and stream exist and returns a Stream object. Panics on error.
func (s *System) MustStream(ctx context.Context, domain, stream string) *Stream {
	out, err := s.Stream(ctx, domain, stream)
	junk.Must(err)
	return out
}

// Stream obtains the event stream for a given domain (app) and stream.  If the domain + stream does not exist yet then
// one is created.
func (s *System) Stream(ctx context.Context, domain, stream string) (*Stream, error) {
	if err := s.Transport.EnsureStream(ctx, domain, stream); err != nil {
		return nil, err
	}
	return &Stream{
		system: s,
		domain: domain,
		stream: stream,
	}, nil
}

// ListDomains returns a list of all available domains.
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

// DomainStreamPair represents a domain and its associated stream.
type DomainStreamPair struct {
	Domain string
	Stream string
}

// ListStreams returns a list of all available domain-stream pairs.
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
				Stream: s,
			})
		}
	}
	return names, nil
}

// NewSystem creates a new System using the provided transport.
func NewSystem(storage Transport) *System {
	return &System{Transport: storage}
}
