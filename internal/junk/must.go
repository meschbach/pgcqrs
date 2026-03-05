// Package junk provides miscellaneous internal utilities.
package junk

// Must panics if the given error is not nil.
func Must(maybeError error) {
	if maybeError != nil {
		panic(maybeError)
	}
}
