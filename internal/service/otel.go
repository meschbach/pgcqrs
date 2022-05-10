package service

import "go.opentelemetry.io/otel"

const tracerName = " git@git.meschbach.com/mee/pgcqrs/internal/service"

var tracer = otel.Tracer(tracerName)
