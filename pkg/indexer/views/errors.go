package views

import "fmt"

// StreamNotFoundError is returned when a projection references a non-existent stream.
type StreamNotFoundError struct {
	Domain string
	Stream string
}

func (e *StreamNotFoundError) Error() string {
	return fmt.Sprintf("stream not found: %s/%s; ensure the stream is created with EnsureStream before using projections", e.Domain, e.Stream)
}
