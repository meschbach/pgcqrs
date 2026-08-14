// Package indexer implements the event-processing pump: acquiring consumer
// locks, watching streams, and dispatching events to indexer handlers.
package indexer

import (
	"time"

	"github.com/cenkalti/backoff/v5"
)

// NewBackoff creates a new backoff instance with default configuration.
// Exponential backoff with jitter, starting at 100ms and maxing at 30s.
func NewBackoff() *backoff.ExponentialBackOff {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = 100 * time.Millisecond
	b.MaxInterval = 30 * time.Second
	b.RandomizationFactor = 0.5
	b.Multiplier = 2
	return b
}
