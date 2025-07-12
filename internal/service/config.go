package service

import (
	"github.com/meschbach/go-junk-bucket/pkg"
	"github.com/meschbach/go-junk-bucket/pkg/observability"
	"github.com/meschbach/pgcqrs/internal"
)

type Config struct {
	Telemetry    observability.Config
	Storage      internal.Storage    `json:"storage"`
	Listener     *ListenerConfig     `json:"listener,omitempty"`
	GRPCListener *GRPCListenerConfig `json:"grpc-listener,omitempty"`
}

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

	if url := pkg.EnvOrDefault("PGCQRS_STORAGE_POSTGRES_URL", ""); url != "" {
		c.Storage.Primary.DatabaseURL = url
	}
}

type ListenerConfig struct {
	Address string     `json:"address"`
	TLS     *TLSConfig `json:"tls,omitempty"`
}

func (l *ListenerConfig) LoadDefaults() {
	l.Address = pkg.EnvOrDefault("PGCQRS_LISTENER_ADDRESS", "localhost:9000")
}

type TLSConfig struct {
	KeyFile         *string `json:"key-file,omitempty"`
	CertificateFile *string `json:"certificate-file,omitempty"`
}

type GRPCListenerConfig struct {
	Address    string            `json:"address"`
	ServicePKI *PKIServiceConfig `json:"service-pki,omitempty"`
}

func (g *GRPCListenerConfig) LoadDefaults() {
	g.Address = pkg.EnvOrDefault("PGCQRS_GRPC_LISTENER_ADDRESS", "0.0.0.0:9001")
}

type PKIServiceConfig struct {
	KeyFile         string `json:"key-file"`
	CertificateFile string `json:"certificate-file"`
}
