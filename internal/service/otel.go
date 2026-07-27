package service

import "go.opentelemetry.io/otel"

const tracerName = " git@git.meschbach.com/mee/pgcqrs/internal/service"

var tracer = otel.Tracer(tracerName)

const watchMeterName = "pgcqrs.internal.service.watch"

// OTel metrics for watch operations.
// Error returns from instrument creation are ignored as these are package-level
// initializations that should not fail in practice.
var (
	watchMeter = otel.Meter(watchMeterName)

	// WatchDedupDrops counts events dropped by the grpcResultStream dedup guard.
	// A non-zero value indicates the SQL AfterID filter returned overlapping
	// results across re-queries, which the dedup guard caught.
	WatchDedupDrops, _ = watchMeter.Int64Counter("watch.dedup.drops") //nolint:errcheck
)
