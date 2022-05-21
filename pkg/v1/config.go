package v1

import (
	"context"
	"fmt"
	"github.com/meschbach/pgcqrs/internal/junk"
)

const (
	TransportTypeMemory = "memory"
	TransportTypeHTTP   = "http"
)

type Config struct {
	//TransportType specifies the underlying transport mechanism to utilize.
	TransportType string `json:"transport-type"`

	//ServiceURL is the URL to connect to for the HTTP service layer
	ServiceURL string `json:"service-url"`
}

func (c *Config) SystemFromConfig() (*System, error) {
	var physical Transport
	switch c.TransportType {
	case TransportTypeMemory:
		physical = NewMemoryTransport()
	case TransportTypeHTTP:
		physical = NewHttpTransport(c.ServiceURL)
	case "": // Original default behavior
		physical = NewHttpTransport(c.ServiceURL)
	default:
		return nil, &UnknownTransportError{TransportType: c.TransportType}
	}
	return NewSystem(physical), nil
}

type UnknownTransportError struct {
	TransportType string
}

func (u *UnknownTransportError) Error() string {
	return fmt.Sprintf("Unknown transport type %q", u.TransportType)
}

type StreamConfig struct {
	Application string `json:"application"`
	Stream      string `json:"stream"`
}

func (s *StreamConfig) Connect(ctx context.Context, system *System) (*Stream, error) {
	return system.Stream(ctx, s.Application, s.Stream)
}

func (s *StreamConfig) MustConnect(ctx context.Context, system *System) *Stream {
	stream, err := s.Connect(ctx, system)
	junk.Must(err)
	return stream
}
