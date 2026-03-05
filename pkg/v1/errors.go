// Package v1 provides the core CQRS client and transport interfaces.
package v1

import "fmt"

// TransportError represents an error that occurred during transport.
type TransportError struct {
	Underlying error
}

func (t *TransportError) Error() string {
	return fmt.Sprintf("transport erorr: %s", t.Underlying.Error())
}

func (t *TransportError) Unwrap() error {
	return t.Underlying
}
