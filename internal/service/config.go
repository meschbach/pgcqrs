package service

import (
	"github.com/meschbach/go-junk-bucket/pkg/observability"
	"github.com/meschbach/pgcqrs/internal"
)

type Config struct {
	Telemetry observability.Config
	Storage   internal.Storage `json:"storage"`
	Listener  *ListenerConfig  `json:"listener,omitempty"`
}

type ListenerConfig struct {
	TLS *TLSConfig `json:"tls,omitempty"`
}

type TLSConfig struct {
	KeyFile         *string `json:"key-file,omitempty"`
	CertificateFile *string `json:"certificate-file,omitempty"`
}
