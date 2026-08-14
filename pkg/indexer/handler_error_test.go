package indexer

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHandlerError_Interface(t *testing.T) {
	t.Parallel()

	t.Run("PlainErrorIsNotRecoverable", func(t *testing.T) {
		t.Parallel()
		err := errors.New("plain error")
		assert.False(t, IsRecoverable(err))
	})

	t.Run("NilErrorIsNotRecoverable", func(t *testing.T) {
		t.Parallel()
		assert.False(t, IsRecoverable(nil))
	})

	t.Run("RecoverableErrorIsRecoverable", func(t *testing.T) {
		t.Parallel()
		err := &RecoverableError{Err: errors.New("transient failure")}
		assert.True(t, IsRecoverable(err))
	})

	t.Run("RecoverableErrorImplementsHandlerError", func(t *testing.T) {
		t.Parallel()
		err := &RecoverableError{Err: errors.New("transient failure")}
		var handlerErr HandlerError = err
		assert.True(t, handlerErr.Recoverable())
	})

	t.Run("RecoverableErrorUnwraps", func(t *testing.T) {
		t.Parallel()
		underlying := errors.New("underlying error")
		err := &RecoverableError{Err: underlying}
		assert.ErrorIs(t, err, underlying)
	})

	t.Run("RecoverableErrorString", func(t *testing.T) {
		t.Parallel()
		err := &RecoverableError{Err: errors.New("test message")}
		assert.Equal(t, "test message", err.Error())
	})
}
