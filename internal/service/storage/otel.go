package storage

import (
	"go.opentelemetry.io/otel"
)

const tracerName = " git@git.meschbach.com/mee/pgcqrs/internal/service/storage"

var tracer = otel.Tracer(tracerName)

const meterName = "pgcqrs.internal.service.storage"

// OTel metrics for consumer lock operations.
// Error returns from instrument creation are ignored as these are package-level
// initializations that should not fail in practice.
var (
	meter = otel.Meter(meterName)

	// AcquireAttempts counts TryAcquire calls.
	AcquireAttempts, _ = meter.Int64Counter("consumer_lock.acquire.attempts") //nolint:errcheck
	// AcquireSuccesses counts locks successfully acquired.
	AcquireSuccesses, _ = meter.Int64Counter("consumer_lock.acquire.successes") //nolint:errcheck
	// AcquireFailures counts locks not acquired (held by another).
	AcquireFailures, _ = meter.Int64Counter("consumer_lock.acquire.failures") //nolint:errcheck
	// ReleaseExplicit counts explicit Release calls (unary + in-stream).
	ReleaseExplicit, _ = meter.Int64Counter("consumer_lock.release.explicit") //nolint:errcheck
	// HeartbeatProcessed counts heartbeats processed.
	HeartbeatProcessed, _ = meter.Int64Counter("consumer_lock.heartbeat.processed") //nolint:errcheck
	// HeartbeatConflict counts heartbeats rejected due to stale position.
	HeartbeatConflict, _ = meter.Int64Counter("consumer_lock.heartbeat.conflict") //nolint:errcheck
	// ExpiryIgnored counts locks treated as expired on access.
	ExpiryIgnored, _ = meter.Int64Counter("consumer_lock.expiry.ignored") //nolint:errcheck
	// StreamClosedNoRelease counts KeepAlive streams closed without Release message.
	StreamClosedNoRelease, _ = meter.Int64Counter("consumer_lock.stream.closed_without_release") //nolint:errcheck
	// AssertionChecks counts lock assertion checks on queries/submits.
	AssertionChecks, _ = meter.Int64Counter("consumer_lock.assertion.checks") //nolint:errcheck
	// AssertionRejections counts requests rejected (lock not held).
	AssertionRejections, _ = meter.Int64Counter("consumer_lock.assertion.rejections") //nolint:errcheck
	// CleanupDeleted counts expired locks removed during cleanup.
	CleanupDeleted, _ = meter.Int64Counter("consumer_lock.cleanup.deleted") //nolint:errcheck

	// activeLocks tracks currently held locks.
	activeLocks, _ = meter.Int64UpDownCounter("consumer_lock.active") //nolint:errcheck

	// holdDuration tracks time between acquire and release/expiry.
	holdDuration, _ = meter.Float64Histogram("consumer_lock.hold_duration.seconds") //nolint:errcheck
	// heartbeatInterval tracks time between heartbeats.
	heartbeatInterval, _ = meter.Float64Histogram("consumer_lock.heartbeat.interval.seconds") //nolint:errcheck
)
