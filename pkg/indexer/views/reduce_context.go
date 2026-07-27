package views

// ReduceContext provides handler dependencies during event processing.
// It gives access to previous entity state via Get and is designed for future expansion.
type ReduceContext struct {
	get func(kind string, key Key) (*Entity, error)
}

// Get retrieves the current entity state for the given kind and key.
// Returns nil entity with no error if the entity does not exist.
func (rc *ReduceContext) Get(kind string, key Key) (*Entity, error) {
	return rc.get(kind, key)
}
