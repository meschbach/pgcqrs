package views

import (
	"context"
	"encoding/json"
	"fmt"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/meschbach/pgcqrs/pkg/v1/query2"
)

// Indexer implements indexer.Indexer for view projections.
// It builds a query2.Query with handlers registered for each OnKind kind,
// dispatching to typed handlers, applying mutations to the Store, and notifying observers.
type Indexer struct {
	projection *Projection
	store      Store
	notifier   *Notifier
	stream     v1.StreamTransport
}

// NewIndexer creates a Indexer with the given projection, store, notifier, and stream.
func NewIndexer(proj *Projection, store Store, notifier *Notifier, stream v1.StreamTransport) *Indexer {
	return &Indexer{
		projection: proj,
		store:      store,
		notifier:   notifier,
		stream:     stream,
	}
}

// Query builds a query2.Query with handlers for each registered OnKind kind.
// Each handler unmarshals the raw JSON, calls the typed handler, applies mutations
// to the Store, and notifies observers.
func (v *Indexer) Query() *query2.Query {
	q := query2.NewQuery(v.stream)

	for kind, rawHandler := range v.projection.handlers {
		kind := kind
		raw := rawHandler

		q.OnKind(kind).Each(v.makeDispatchHandler(kind, raw))
	}

	return q
}

// makeDispatchHandler creates a query2 handler that wraps the raw handler
// with Store.Persist and Notifier.Notify calls. Every processed event is
// persisted (advancing the projection version, even when the handler produced
// no mutations) and broadcast to observers.
func (v *Indexer) makeDispatchHandler(kind string, raw rawHandler) v1.OnStreamQueryResult {
	return func(ctx context.Context, e v1.Envelope, rawJSON json.RawMessage) error {
		actx := &ReduceContext{
			get: func(kind string, key Key) (*Entity, error) {
				return v.store.Get(ctx, kind, key)
			},
		}

		result, err := raw(ctx, e, rawJSON, actx)
		if err != nil {
			return fmt.Errorf("handler for %s: %w", kind, err)
		}

		change, err := v.store.Persist(ctx, result, e.ID)
		if err != nil {
			return fmt.Errorf("persist mutations for %s: %w", kind, err)
		}

		if v.notifier != nil && change != nil {
			if err := v.notifier.Emit(ctx, *change); err != nil {
				return fmt.Errorf("notify observers for %s: %w", kind, err)
			}
		}

		return nil
	}
}
