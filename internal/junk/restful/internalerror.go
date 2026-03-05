// Package restful provides common HTTP response helpers.
package restful

import (
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel/trace"
)

const debug = false

// InternalError writes an internal server error response.
func InternalError(writer http.ResponseWriter, request *http.Request, problem error) {
	ctx := request.Context()
	span := trace.SpanFromContext(ctx)
	span.RecordError(problem)
	if debug {
		fmt.Printf("Error:\n%s\n", problem.Error())
	}
	respondString(ctx, writer, 500, "Internal error")
}
