package v1

import "go.opentelemetry.io/otel"

const tracerName = "git.meschbach.com/mee/pgcqrs/client/v1"

var tracer = otel.Tracer(tracerName)
