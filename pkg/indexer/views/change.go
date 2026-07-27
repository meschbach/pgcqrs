package views

// Change represents a batch of mutations from a single event,
// used for streaming notifications and gRPC responses.
type Change struct {
	Upserts []Upsert
	Deletes []Delete
	Version int64
}
