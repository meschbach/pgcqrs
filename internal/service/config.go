package service

import (
	"github.com/meschbach/pgcqrs/internal"
	"github.com/meschbach/pgcqrs/internal/junk/telemetry"
)

type Config struct {
	Telemetry telemetry.Config
	Storage   internal.Storage `json:"storage"`
}
