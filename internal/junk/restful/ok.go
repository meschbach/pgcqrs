package restful

import (
	"encoding/json"
	"go.opentelemetry.io/otel/trace"
	"net/http"
)

func Ok(writer http.ResponseWriter, request *http.Request, entity interface{}) {
	ctx := request.Context()
	//todo: encode to things other than JSON
	header := writer.Header()
	header.Add("Content-Type", "application/json")
	writer.WriteHeader(200)

	out := json.NewEncoder(writer)
	if err := out.Encode(entity); err != nil {
		span := trace.SpanFromContext(ctx)
		span.AddEvent("Failed JSON encoding")
		span.RecordError(err)
	}
}
