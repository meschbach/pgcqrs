package migrator

import "github.com/meschbach/pgcqrs/internal"

type Config struct {
	Storage      internal.Storage `json:"storage"`
	MigrationDir string           `json:"migration-dir"`
}
