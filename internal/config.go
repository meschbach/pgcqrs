// Package internal contains internal configuration and types.
package internal

// PGStorage represents the PostgreSQL storage configuration.
type PGStorage struct {
	DatabaseURL string `json:"url"`
}

// Storage represents the storage configuration for the application.
type Storage struct {
	Primary PGStorage `json:"primary"`
}
