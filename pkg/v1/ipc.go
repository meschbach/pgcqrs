package v1

import "time"

type Submitted struct {
	// ID is the Envelope ID of the event.
	ID int64
}

type Envelope struct {
	ID   int64  `json:"id"`
	When string `json:"when"`
	Kind string `json:"kind"`
}

func (e Envelope) InternalizeWhen() (time.Time, error) {
	return time.Parse(time.RFC3339Nano, e.When)
}

func FormatEnvelopeWhen(t time.Time) string {
	return t.Format(time.RFC3339Nano)
}
