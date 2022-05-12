package service

import (
	"github.com/meschbach/go-junk-bucket/pkg/observability"
	"github.com/meschbach/pgcqrs/internal"
)

type Config struct {
	Telemetry observability.Config
	Storage   internal.Storage `json:"storage"`
}
