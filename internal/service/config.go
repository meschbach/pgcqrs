package service

import (
	"os"
	"strconv"

	"github.com/meschbach/go-junk-bucket/pkg"
	"github.com/meschbach/go-junk-bucket/pkg/observability"
	"github.com/meschbach/pgcqrs/internal"
)

// Config represents the service configuration.
type Config struct {
	Telemetry    observability.Config
	Storage      internal.Storage    `json:"storage"`
	Listener     *ListenerConfig     `json:"listener,omitempty"`
	GRPCListener *GRPCListenerConfig `json:"grpc-listener,omitempty"`
	OpsListener  *OpsListenerConfig  `json:"ops-listener,omitempty"`
}

// LoadDefaults populates the configuration with default values.
func (c *Config) LoadDefaults() {
	c.Telemetry = observability.DefaultConfig("pgcqrs")
	if c.Listener == nil {
		c.Listener = &ListenerConfig{}
	}
	c.Listener.LoadDefaults()
	if c.GRPCListener == nil {
		c.GRPCListener = &GRPCListenerConfig{}
	}
	c.GRPCListener.LoadDefaults()

	if c.OpsListener == nil {
		c.OpsListener = &OpsListenerConfig{}
	}
	c.OpsListener.LoadDefaults()

	if url := pkg.EnvOrDefault("PGCQRS_STORAGE_POSTGRES_URL", ""); url != "" {
		c.Storage.Primary.DatabaseURL = url
	}
}

// ListenerConfig represents the network listener configuration.
type ListenerConfig struct {
	Address string     `json:"address"`
	TLS     *TLSConfig `json:"tls,omitempty"`
}

// LoadDefaults populates the listener configuration with default values.
func (l *ListenerConfig) LoadDefaults() {
	l.Address = pkg.EnvOrDefault("PGCQRS_LISTENER_ADDRESS", "localhost:9000")
}

// TLSConfig represents the TLS configuration.
type TLSConfig struct {
	KeyFile         *string `json:"key-file,omitempty"`
	CertificateFile *string `json:"certificate-file,omitempty"`
}

// GRPCListenerConfig represents the gRPC listener configuration.
type GRPCListenerConfig struct {
	Address    string            `json:"address"`
	ServicePKI *PKIServiceConfig `json:"service-pki,omitempty"`
}

// LoadDefaults populates the gRPC listener configuration with default values.
func (g *GRPCListenerConfig) LoadDefaults() {
	g.Address = pkg.EnvOrDefault("PGCQRS_GRPC_LISTENER_ADDRESS", "0.0.0.0:9001")
}

// OpsListenerConfig represents the ops listener configuration.
type OpsListenerConfig struct {
	Address string `json:"address"`
	Enabled *bool  `json:"enabled,omitempty"`
}

// LoadDefaults populates the ops listener configuration with default values.
func (o *OpsListenerConfig) LoadDefaults() {
	o.Address = pkg.EnvOrDefault("PGCQRS_OPS_LISTENER_ADDRESS", "127.0.0.1:9002")
	enabled := true
	o.Enabled = &enabled
	if value, ok := os.LookupEnv("PGCQRS_OPS_LISTENER_ENABLED"); ok {
		if parsed, err := strconv.ParseBool(value); err == nil {
			enabled = parsed
			o.Enabled = &enabled
		}
	}
}

// PKIServiceConfig represents the PKI configuration for a service.
type PKIServiceConfig struct {
	KeyFile         string `json:"key-file"`
	CertificateFile string `json:"certificate-file"`
}
