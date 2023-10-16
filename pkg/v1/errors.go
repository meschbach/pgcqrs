package v1

import "fmt"

type TransportError struct {
	Underlying error
}

func (t *TransportError) Error() string {
	return fmt.Sprintf("transport erorr: %s", t.Underlying.Error())
}

func (t *TransportError) Unwrap() error {
	return t.Underlying
}
