package views

import "fmt"

// Key represents an entity key with 1 or 2 string parts.
// Simple entities use a single key; composite entities use two.
type Key struct {
	parts []string
}

// NewKey creates a new Key with the given parts.
// Supports 1 or 2 string parts. Panics if more than 2 are provided (programmer error).
func NewKey(parts ...string) Key {
	if len(parts) > 2 {
		panic(fmt.Sprintf("views.NewKey: expected 1 or 2 key parts, got %d", len(parts)))
	}
	return Key{parts: parts}
}

// Parts returns the key parts (1 or 2 elements).
func (k Key) Parts() []string {
	return k.parts
}

// IsComposite returns true if the key has two parts.
func (k Key) IsComposite() bool {
	return len(k.parts) == 2
}

// IsSingle returns true if the key has one part.
func (k Key) IsSingle() bool {
	return len(k.parts) == 1
}

// validateKey returns an error if the key does not have exactly 1 or 2 parts.
func validateKey(key Key) error {
	n := len(key.Parts())
	if n == 0 || n > 2 {
		return fmt.Errorf("views: key must have 1 or 2 parts, got %d", n)
	}
	return nil
}
