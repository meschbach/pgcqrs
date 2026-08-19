package views

// Upsert represents an entity upsert mutation.
// The framework marshals Value to JSON internally, so handlers can pass typed structs directly.
type Upsert struct {
	Kind  string
	Key   Key
	Value any
}

// Delete represents an entity delete mutation.
type Delete struct {
	Kind string
	Key  Key
}

// ReduceResult contains the mutations produced by a single event handler.
type ReduceResult struct {
	Upserts []Upsert
	Deletes []Delete
}
