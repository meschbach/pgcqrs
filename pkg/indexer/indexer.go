package indexer

import "github.com/meschbach/pgcqrs/pkg/v1/query2"

// Indexer defines the interface for event processing.
// Each indexer type builds a query2.Query with handlers registered.
// The Pump drives the query's Watch loop.
type Indexer interface {
	Query() *query2.Query
}
