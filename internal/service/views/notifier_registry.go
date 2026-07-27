package views

import (
	"sync"

	"github.com/meschbach/pgcqrs/pkg/indexer/views"
)

// NotifierRegistry scopes change notifications per stream.
// Each stream_id gets its own Notifier, ensuring WatchChanges and UntilVersion
// subscriptions only receive notifications for their own stream.
type NotifierRegistry struct {
	mu        sync.Mutex
	notifiers map[int64]*views.Notifier
}

// NewNotifierRegistry creates a new NotifierRegistry.
func NewNotifierRegistry() *NotifierRegistry {
	return &NotifierRegistry{
		notifiers: make(map[int64]*views.Notifier),
	}
}

// getOrCreate returns the notifier for the given stream, creating one if needed.
func (r *NotifierRegistry) getOrCreate(streamID int64) *views.Notifier {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.notifiers[streamID]
	if !ok {
		n = views.NewNotifier()
		r.notifiers[streamID] = n
	}
	return n
}

// Notify fires all callbacks registered for the given stream.
func (r *NotifierRegistry) Notify(streamID int64, change views.Change) {
	n := r.getOrCreate(streamID)
	n.Notify(change)
}

// OnChange registers a callback for changes on the given stream.
// Returns an unsubscribe function.
func (r *NotifierRegistry) OnChange(streamID int64, fn func(views.Change)) func() {
	n := r.getOrCreate(streamID)
	return n.OnChange(fn)
}
