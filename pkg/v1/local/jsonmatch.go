// Package local provides local (in-memory) matching utilities.
package local

import (
	"encoding/json"

	"github.com/nsf/jsondiff"
)

// JSONIsSubset returns true if the subset is a subset of the superset.
func JSONIsSubset(superset, subset json.RawMessage) bool {
	result, _ := jsondiff.Compare(superset, subset, &jsondiff.Options{})
	return result == jsondiff.SupersetMatch || result == jsondiff.FullMatch
}
