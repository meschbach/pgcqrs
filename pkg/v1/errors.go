// Package v1 provides the core CQRS client and transport interfaces.
package v1

import (
	"fmt"

	"github.com/meschbach/pgcqrs/pkg/ipc"
)

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

// EmptyQueryError is returned when a QueryIn has no selection clauses.
// At least one of OnKind, OnID, or OnEach must be provided.
type EmptyQueryError struct{}

func (e *EmptyQueryError) Error() string {
	return "query has no selection clause; provide at least one of OnKind, OnID, or OnEach"
}

// ValidateQuery checks that the QueryIn has at least one selection clause.
// Returns EmptyQueryError if OnKind, OnID, and OnEach are all empty/nil.
func ValidateQuery(q *ipc.QueryIn) error {
	if len(q.OnKind) == 0 && len(q.OnID) == 0 && q.OnEach == nil {
		return &EmptyQueryError{}
	}
	return nil
}
