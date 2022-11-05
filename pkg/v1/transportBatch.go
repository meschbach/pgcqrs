package v1

import "encoding/json"

type WireBatchResults struct {
	//Page encapsulates a single data page
	Page     []WireBatchResultPair `json:"page"`
	Features *WireFeaturesSupport  `json:"features,omitempty"`
}

type WireBatchResultPair struct {
	Meta Envelope        `json:"meta"`
	Data json.RawMessage `json:"event"`
}

type WireFeaturesSupport struct {
	Disjoints bool `json:"kind-disjoints,omitempty"`
}
