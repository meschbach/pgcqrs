package v1

import "encoding/json"

// WireBatchR2Request is the second revision of a batch query operation.  Revision 2 is designed to dispatch various
// matches to discrete handlers via integer identifiers.
type WireBatchR2Request struct {
	//OnKinds specifies the operations ot perform on the desired kinds
	OnKinds []WireBatchR2KindQuery `json:"kinds"`
	//OnID allows recalling events with a specific ID
	OnID []WireBatchR2IDQuery `json:"ids"`
}

func (w WireBatchR2Request) Empty() bool {
	return len(w.OnKinds) == 0 && len(w.OnID) == 0
}

// WireBatchR2KindQuery describes matching a specific kind of entity.  Either all or in part.
type WireBatchR2KindQuery struct {
	//Kind is the name of the type to be matched
	Kind string `json:"kind"`
	//If all is specified the given match will be provided with all matching kinds.
	All *int `json:"all,omitempty"`
	//Match describes disjoint matches ('or') operations.  A single document may be dispatched multiple times if it meets multiple alternatives.
	Match []WireBatchR2KindMatch `json:"match,omitempty"`
}

// WireBatchR2KindMatch describes how to match a specific subset of documents for the given kind.
type WireBatchR2KindMatch struct {
	//Op to be dispatched on match
	Op int `json:"op"`
	//If the provided document is a subset of the event document then a match is considered to have occurred.
	Subset json.RawMessage `json:"$sub"`
}

// WireBatchR2IDQuery will result in a dispatched being rendered for the given event ID.
type WireBatchR2IDQuery struct {
	Op int   `json:"op"`
	ID int64 `json:"id"`
}

// WireBatchR2Result contains the matched documents and possibly extension information.
type WireBatchR2Result struct {
	Results []WireBatchR2Dispatch `json:"results"`
}

// WireBatchR2Dispatch informs of a specific match for the intended operation.
type WireBatchR2Dispatch struct {
	Envelope Envelope        `json:"n"`
	Event    json.RawMessage `json:"v"`
	Op       int             `json:"o"`
}
