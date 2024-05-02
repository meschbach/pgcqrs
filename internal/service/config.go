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
	Address string `json:"address"`
}
