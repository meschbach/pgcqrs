package v1

import "encoding/json"

// WireBatchResults represents the results of a batch query.
type WireBatchResults struct {
	// Page encapsulates a single data page
	Page []WireBatchResultPair `json:"page"`
}

// WireBatchResultPair represents a single result pair in a batch query.
type WireBatchResultPair struct {
	Meta Envelope        `json:"meta"`
	Data json.RawMessage `json:"event"`
}
