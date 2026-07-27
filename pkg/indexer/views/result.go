package views

import "fmt"

// Status indicates the result of a version-constrained Get operation.
type Status int

func (s *Status) String() string {
	switch *s {
	case StatusOK:
		return "ok"
	case StatusNotFound:
		return "not found"
	case StatusStale:
		return "stale"
	case StatusTimeout:
		return "timeout"
	default:
		return fmt.Sprintf("%#v", *s)
	}
}

const (
	// StatusOK indicates the version constraint was satisfied.
	StatusOK Status = iota
	// StatusNotFound indicates the entity does not exist.
	StatusNotFound
	// StatusStale indicates the projection is behind the requested version (After failed).
	StatusStale
	// StatusTimeout indicates the wait for the target version timed out (UntilVersion failed).
	StatusTimeout
)

// Result contains the entity and version constraint status from a Get operation.
type Result struct {
	Entity  *Entity
	Version int64
	Status  Status
}
