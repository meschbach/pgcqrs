package restful

import (
	"context"
	"go.opentelemetry.io/otel/trace"
	"net/http"
)

//respondString responds on the given response with the status code and text body.  If an error occurs while responding
//it is journaled unless it is a client error.
func respondString(ctx context.Context, writer http.ResponseWriter, status int, body string) {
	writer.WriteHeader(status)
	if _, err := writer.Write([]byte(body)); err != nil {
		//TODO: only record if not a client error
		span := trace.SpanFromContext(ctx)
		span.AddEvent("failed to write response")
		span.RecordError(err)
	}
}
