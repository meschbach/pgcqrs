package restful

import (
	"fmt"
	"go.opentelemetry.io/otel/trace"
	"net/http"
)

const debug = false

func InternalError(writer http.ResponseWriter, request *http.Request, problem error) {
	ctx := request.Context()
	span := trace.SpanFromContext(ctx)
	span.RecordError(problem)
	if debug {
		fmt.Printf("Error:\n%s\n", problem.Error())
	}
	respondString(ctx, writer, 500, "Internal error")
}
