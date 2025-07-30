package restful

import (
	"fmt"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"net/http"
)

func ClientError(writer http.ResponseWriter, request *http.Request, problem error) {
	ctx := request.Context()
	span := trace.SpanFromContext(ctx)
	span.SetStatus(codes.Error, problem.Error())
	span.RecordError(problem)
	if debug {
		fmt.Printf("Error:\n%s\n", problem.Error())
	}
	respondString(ctx, writer, 400, "Client error")
}
