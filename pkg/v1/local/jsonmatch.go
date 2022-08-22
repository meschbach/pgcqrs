package local

import (
	"encoding/json"
	"github.com/nsf/jsondiff"
)

func JSONIsSubset(superset json.RawMessage, subset json.RawMessage) bool {
	result, _ := jsondiff.Compare(superset, subset, &jsondiff.Options{})
	return result == jsondiff.SupersetMatch || result == jsondiff.FullMatch
}
