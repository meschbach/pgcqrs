package indexer

// PumpState represents the current state of the pump lifecycle.
type PumpState int

const (
	// PumpStateWaitingForLock indicates the pump is blocked waiting to acquire the lock.
	PumpStateWaitingForLock PumpState = iota
	// PumpStateAcquiring indicates the lock has been acquired and the pump is setting up the watch.
	PumpStateAcquiring
	// PumpStateWatching indicates the pump is actively processing events.
	PumpStateWatching
	// PumpStateLockLost indicates the pump detected lock loss and will re-acquire.
	PumpStateLockLost
	// PumpStateFailed indicates an unrecoverable error occurred.
	PumpStateFailed
	// PumpStateClosed indicates the pump context is done and it's exiting cleanly.
	PumpStateClosed
)

// String returns a human-readable representation of the pump state.
func (s PumpState) String() string {
	switch s {
	case PumpStateWaitingForLock:
		return "WaitingForLock"
	case PumpStateAcquiring:
		return "Acquiring"
	case PumpStateWatching:
		return "Watching"
	case PumpStateLockLost:
		return "LockLost"
	case PumpStateFailed:
		return "Failed"
	case PumpStateClosed:
		return "Closed"
	default:
		return "Unknown"
	}
}

// PumpStateEvent represents a state transition in the pump lifecycle.
type PumpStateEvent struct {
	State    PumpState
	Previous PumpState
	Error    error
}
