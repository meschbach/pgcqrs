package indexer

import "go.opentelemetry.io/otel"

const tracerName = "github.com/meschbach/pgcqrs/pkg/indexer"

var tracer = otel.Tracer(tracerName)
