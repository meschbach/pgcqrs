package views

import (
	"context"
	"sync"

	"github.com/meschbach/go-junk-bucket/pkg/emitter"
)

// Notifier is an in-process callback registry for change notifications.
// One Notifier per projection, created by newClient.
// Indexer calls Emit after each apply.
// Client.OnChange and UntilVersion subscribe via OnChange.
type Notifier struct {
	mu       sync.Mutex
	onChange *emitter.MutexDispatcher[Change]
	// lastVersion is the highest version broadcast so far. It starts at -1,
	// meaning no change has been broadcast yet; event IDs are non-negative, so
	// -1 is unreachable and keeps a first change at version 0 distinguishable
	// from a notifier that has never seen an event.
	lastVersion int64
}

// NewNotifier creates a new in-process change notifier.
func NewNotifier() *Notifier {
	return &Notifier{onChange: emitter.NewMutexDispatcher[Change](), lastVersion: -1}
}

// Emit fires all registered callbacks with the given change. Callback errors
// are aggregated and returned; a panicking callback is contained and reported
// as an error rather than aborting delivery to the remaining callbacks.
func (n *Notifier) Emit(ctx context.Context, c Change) error {
	n.mu.Lock()
	if c.Version > n.lastVersion {
		n.lastVersion = c.Version
	}
	n.mu.Unlock()
	return n.onChange.Emit(ctx, c)
}

// OnChange registers a callback to be called when changes are applied.
// Returns an unsubscribe function that removes the callback when called.
func (n *Notifier) OnChange(fn func(context.Context, Change) error) func() {
	sub := n.onChange.OnE(fn)
	return func() {
		n.onChange.Off(sub)
	}
}

// WaitForVersion blocks until the projection reaches at least targetVersion or
// ctx is done. It returns (true, nil) once a change with Version >= targetVersion
// has been applied, or (false, ctx.Err()) when ctx ends first.
//
// The subscription is registered before checking the last broadcast version, so a
// projection that already reached the target before the wait began is detected
// immediately rather than missing the notification and blocking until ctx ends.
//
// lastVersion starts at -1 (no events processed), which is below every real
// event ID, so a fresh notifier never satisfies a wait. The first change at
// version 0 raises it to 0, making that version genuinely reachable.
func (n *Notifier) WaitForVersion(ctx context.Context, targetVersion int64) (bool, error) {
	ch := make(chan Change, 16)
	unsubscribe := n.OnChange(func(_ context.Context, c Change) error {
		select {
		case ch <- c:
		default:
		}
		return nil
	})
	defer unsubscribe()

	if n.atOrAbove(targetVersion) {
		return true, nil
	}

	for {
		select {
		case c := <-ch:
			if c.Version >= targetVersion {
				return true, nil
			}
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}

func (n *Notifier) atOrAbove(targetVersion int64) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.lastVersion >= targetVersion
}
