package v1

import (
	"context"
	"fmt"

	"github.com/meschbach/go-junk-bucket/pkg"
	"github.com/meschbach/pgcqrs/internal/junk"
)

const (
	// TransportTypeMemory specifies an in-memory transport.
	TransportTypeMemory = "memory"
	// TransportTypeHTTP specifies an HTTP transport.
	TransportTypeHTTP = "http"
	// TransportTypeGRPC specifies a gRPC transport.
	TransportTypeGRPC = "grpc"
)

// Config represents the client configuration.
type Config struct {
	// TransportType specifies the underlying transport mechanism to utilize.
	TransportType string `json:"transport-type"`

	// ServiceURL is the URL to connect to for the HTTP service layer
	ServiceURL string `json:"service-url"`
}

// NewConfig creates a new client configuration with default values.
func NewConfig() *Config {
	cfg := &Config{}
	cfg.ServiceURL = "http://localhost:9000"
	cfg.TransportType = TransportTypeHTTP
	return cfg
}

// LoadEnv loads configuration from environment variables using default prefixes.
func (c *Config) LoadEnv() *Config {
	return c.LoadEnvWithPrefix("")
}

// LoadEnvWithPrefix loads configuration from environment variables with the given prefix.
func (c *Config) LoadEnvWithPrefix(prefix string) *Config {
	c.ServiceURL = pkg.EnvOrDefault(prefix+"PGCQRS_SERVICE_URL", c.ServiceURL)
	c.TransportType = pkg.EnvOrDefault(prefix+"PGCQRS_SERVICE_TRANSPORT", c.TransportType)
	fmt.Printf("Using %q for service transport\n", c.TransportType)
	return c
}

// SystemFromConfig creates a new System from the configuration.
func (c *Config) SystemFromConfig() (*System, error) {
	var physical Transport
	switch c.TransportType {
	case TransportTypeMemory:
		physical = NewMemoryTransport()
	case TransportTypeHTTP:
		physical = NewHTTPTransport(c.ServiceURL)
	case "": // Original default behavior
		physical = NewHTTPTransport(c.ServiceURL)
	case TransportTypeGRPC:
		var err error
		physical, err = NewGRPCTransport(c.ServiceURL)
		if err != nil {
			return nil, err
		}
	default:
		return nil, &UnknownTransportError{TransportType: c.TransportType}
	}
	return NewSystem(physical), nil
}

// UnknownTransportError represents an error for an unrecognized transport type.
type UnknownTransportError struct {
	TransportType string
}

func (u *UnknownTransportError) Error() string {
	return fmt.Sprintf("Unknown transport type %q", u.TransportType)
}

// StreamConfig represents the configuration for a specific stream.
type StreamConfig struct {
	Application string `json:"application"`
	Stream      string `json:"stream"`
}

// Connect establishes a connection to the configured stream.
func (s *StreamConfig) Connect(ctx context.Context, system *System) (*Stream, error) {
	return system.Stream(ctx, s.Application, s.Stream)
}

// MustConnect establishes a connection to the configured stream and panics on error.
func (s *StreamConfig) MustConnect(ctx context.Context, system *System) *Stream {
	stream, err := s.Connect(ctx, system)
	junk.Must(err)
	return stream
}
