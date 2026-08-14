package indexer

// HandlerError is returned by handlers to indicate error semantics.
// Handlers can implement this interface to control pump behavior on errors.
type HandlerError interface {
	error
	Recoverable() bool
}

// RecoverableError wraps an error to indicate it's recoverable.
// When a handler returns this error, the pump will transition to LockLost
// and attempt re-acquisition + re-watch after backoff.
type RecoverableError struct {
	Err error
}

func (e *RecoverableError) Error() string {
	return e.Err.Error()
}

func (e *RecoverableError) Unwrap() error {
	return e.Err
}

// Recoverable reports that the wrapped error should trigger lock re-acquisition.
func (e *RecoverableError) Recoverable() bool {
	return true
}

// IsRecoverable checks if an error implements HandlerError and is recoverable.
// Returns false for plain errors or HandlerError with Recoverable() == false.
func IsRecoverable(err error) bool {
	if err == nil {
		return false
	}
	if handlerErr, ok := err.(HandlerError); ok {
		return handlerErr.Recoverable()
	}
	return false
}
