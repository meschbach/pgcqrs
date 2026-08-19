package views

import (
	"context"
	"sync"

	"github.com/meschbach/pgcqrs/pkg/indexer/views"
)

// NotifierRegistry scopes change notifications per (stream, projection).
// Each projection gets its own Notifier, ensuring WatchChanges and UntilVersion
// subscriptions only receive notifications for their own projection. Scoping by
// projection (not just stream) is required because multiple projections on the
// same stream advance versions independently.
type NotifierRegistry struct {
	mu        sync.Mutex
	notifiers map[notifierKey]*views.Notifier
}

type notifierKey struct {
	streamID   int64
	projection string
}

// NewNotifierRegistry creates a new NotifierRegistry.
func NewNotifierRegistry() *NotifierRegistry {
	return &NotifierRegistry{
		notifiers: make(map[notifierKey]*views.Notifier),
	}
}

// getOrCreate returns the notifier for the given stream and projection, creating one if needed.
func (r *NotifierRegistry) getOrCreate(streamID int64, projection string) *views.Notifier {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := notifierKey{streamID: streamID, projection: projection}
	n, ok := r.notifiers[key]
	if !ok {
		n = views.NewNotifier()
		r.notifiers[key] = n
	}
	return n
}

// Notify fires all callbacks registered for the given stream and projection.
func (r *NotifierRegistry) Notify(ctx context.Context, streamID int64, projection string, change views.Change) error {
	n := r.getOrCreate(streamID, projection)
	return n.Emit(ctx, change)
}

// OnChange registers a callback for changes on the given stream and projection.
// Returns an unsubscribe function.
func (r *NotifierRegistry) OnChange(streamID int64, projection string, fn func(context.Context, views.Change) error) func() {
	n := r.getOrCreate(streamID, projection)
	return n.OnChange(fn)
}

// WaitForVersion blocks until the given projection reaches at least targetVersion
// or ctx is done. See views.Notifier.WaitForVersion.
func (r *NotifierRegistry) WaitForVersion(ctx context.Context, streamID int64, projection string, targetVersion int64) (bool, error) {
	n := r.getOrCreate(streamID, projection)
	return n.WaitForVersion(ctx, targetVersion)
}
