package storage

import "go.opentelemetry.io/otel"

const tracerName = " git@git.meschbach.com/mee/pgcqrs/internal/service/storage"

var tracer = otel.Tracer(tracerName)
