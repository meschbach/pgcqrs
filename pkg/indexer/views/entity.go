package views

import "encoding/json"

// Entity represents a stored projected entity with its kind, key, value, and version.
type Entity struct {
	Kind    string
	Key     Key
	Value   json.RawMessage
	Version int64
}
