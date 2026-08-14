package indexer

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	meterName = "github.com/meschbach/pgcqrs/pkg/indexer"
)

// pumpMetrics holds the OpenTelemetry instruments used by a Pump.
type pumpMetrics struct {
	stateTransitions    metric.Int64Counter
	lockLossEvents      metric.Int64Counter
	watchStreamFailures metric.Int64Counter
}

// newPumpMetrics registers the pump instruments with the given meter. A failed
// registration is fatal: a pump without its metrics would be silently
// unobservable.
func newPumpMetrics(meter metric.Meter) *pumpMetrics {
	var err error
	m := &pumpMetrics{}

	m.stateTransitions, err = meter.Int64Counter(
		"pump.state_transitions",
		metric.WithDescription("Number of pump state transitions"),
		metric.WithUnit("{transition}"),
	)
	if err != nil {
		panic(err)
	}

	m.lockLossEvents, err = meter.Int64Counter(
		"pump.lock_loss_events",
		metric.WithDescription("Number of lock loss events"),
		metric.WithUnit("{event}"),
	)
	if err != nil {
		panic(err)
	}

	m.watchStreamFailures, err = meter.Int64Counter(
		"pump.watch_stream_failures",
		metric.WithDescription("Number of watch stream failures"),
		metric.WithUnit("{failure}"),
	)
	if err != nil {
		panic(err)
	}

	return m
}

// recordStateTransition records a state transition metric.
func (m *pumpMetrics) recordStateTransition(ctx context.Context, state PumpState) {
	m.stateTransitions.Add(ctx, 1, metric.WithAttributes(
		attribute.String("state", state.String()),
	))
}

// recordLockLossEvent records a lock loss event metric.
func (m *pumpMetrics) recordLockLossEvent(ctx context.Context) {
	m.lockLossEvents.Add(ctx, 1)
}

// recordWatchStreamFailure records a watch stream failure metric.
func (m *pumpMetrics) recordWatchStreamFailure(ctx context.Context) {
	m.watchStreamFailures.Add(ctx, 1)
}
