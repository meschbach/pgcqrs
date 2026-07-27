package views

import "sync"

// Notifier is an in-process callback registry for change notifications.
// One Notifier per projection, created by Connect/ConnectMemory.
// Indexer calls Notify after each apply.
// Client.OnChange and UntilVersion subscribe via OnChange.
type Notifier struct {
	mu        sync.Mutex
	callbacks map[int]func(Change)
	nextID    int
}

// NewNotifier creates a new in-process change notifier.
func NewNotifier() *Notifier {
	return &Notifier{callbacks: make(map[int]func(Change))}
}

// Notify fires all registered callbacks with the given change.
func (n *Notifier) Notify(c Change) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, fn := range n.callbacks {
		fn(c)
	}
}

// OnChange registers a callback to be called when changes are applied.
// Returns an unsubscribe function that removes the callback when called.
func (n *Notifier) OnChange(fn func(Change)) func() {
	n.mu.Lock()
	defer n.mu.Unlock()
	id := n.nextID
	n.nextID++
	n.callbacks[id] = fn
	return func() {
		n.mu.Lock()
		defer n.mu.Unlock()
		delete(n.callbacks, id)
	}
}
