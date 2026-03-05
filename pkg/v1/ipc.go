package v1

import "time"

// Submitted represents a successfully submitted event.
type Submitted struct {
	// ID is the Envelope ID of the event.
	ID int64
}

// Envelope contains the metadata for a single event.
type Envelope struct {
	ID   int64  `json:"id"`
	When string `json:"when"`
	Kind string `json:"kind"`
}

// InternalizeWhen converts the RFC3339 formatted time string into a time.Time.
func (e Envelope) InternalizeWhen() (time.Time, error) {
	return time.Parse(time.RFC3339Nano, e.When)
}

// FormatEnvelopeWhen formats a time.Time into the expected RFC3339 string format.
func FormatEnvelopeWhen(t time.Time) string {
	return t.Format(time.RFC3339Nano)
}
