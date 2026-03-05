package migrator

import "github.com/meschbach/pgcqrs/internal"

// Config represents the migrator configuration.
type Config struct {
	Storage internal.Storage `json:"storage"`
}
