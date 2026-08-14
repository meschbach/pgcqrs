package indexer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestStateTransitionMetrics(t *testing.T) {
	t.Parallel()

	// Create a metric reader to capture metrics
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	pm := newPumpMetrics(provider.Meter(meterName))

	// Record some state transitions
	ctx := t.Context()
	pm.recordStateTransition(ctx, PumpStateWaitingForLock)
	pm.recordStateTransition(ctx, PumpStateAcquiring)
	pm.recordStateTransition(ctx, PumpStateWatching)
	pm.recordStateTransition(ctx, PumpStateLockLost)
	pm.recordStateTransition(ctx, PumpStateWatching)

	// Collect metrics
	rm := &metricdata.ResourceMetrics{}
	err := reader.Collect(ctx, rm)
	require.NoError(t, err)

	// Verify metrics were recorded
	assert.Len(t, rm.ScopeMetrics, 1)
	assert.Len(t, rm.ScopeMetrics[0].Metrics, 1)

	metric := rm.ScopeMetrics[0].Metrics[0]
	assert.Equal(t, "pump.state_transitions", metric.Name)

	// Verify the data points
	data, ok := metric.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	// We have 4 unique states, so 4 data points (aggregated by state label)
	assert.Len(t, data.DataPoints, 4)

	// Verify state labels and counts
	states := make(map[string]int64)
	for _, dp := range data.DataPoints {
		stateAttr, _ := dp.Attributes.Value(attribute.Key("state"))
		states[stateAttr.AsString()] = dp.Value
	}

	assert.Equal(t, int64(1), states["WaitingForLock"])
	assert.Equal(t, int64(1), states["Acquiring"])
	assert.Equal(t, int64(2), states["Watching"])
	assert.Equal(t, int64(1), states["LockLost"])
}

func TestLockLossMetrics(t *testing.T) {
	t.Parallel()

	// Create a metric reader to capture metrics
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	pm := newPumpMetrics(provider.Meter(meterName))

	// Record some lock loss events
	ctx := t.Context()
	pm.recordLockLossEvent(ctx)
	pm.recordLockLossEvent(ctx)
	pm.recordLockLossEvent(ctx)

	// Collect metrics
	rm := &metricdata.ResourceMetrics{}
	err := reader.Collect(ctx, rm)
	require.NoError(t, err)

	// Verify metrics were recorded
	assert.Len(t, rm.ScopeMetrics, 1)
	assert.Len(t, rm.ScopeMetrics[0].Metrics, 1)

	metric := rm.ScopeMetrics[0].Metrics[0]
	assert.Equal(t, "pump.lock_loss_events", metric.Name)

	// Verify the data points
	data, ok := metric.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	assert.Len(t, data.DataPoints, 1)
	assert.Equal(t, int64(3), data.DataPoints[0].Value)
}

func TestWatchStreamFailureMetrics(t *testing.T) {
	t.Parallel()

	// Create a metric reader to capture metrics
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	pm := newPumpMetrics(provider.Meter(meterName))

	// Record some watch stream failures
	ctx := t.Context()
	pm.recordWatchStreamFailure(ctx)
	pm.recordWatchStreamFailure(ctx)

	// Collect metrics
	rm := &metricdata.ResourceMetrics{}
	err := reader.Collect(ctx, rm)
	require.NoError(t, err)

	// Verify metrics were recorded
	assert.Len(t, rm.ScopeMetrics, 1)
	assert.Len(t, rm.ScopeMetrics[0].Metrics, 1)

	metric := rm.ScopeMetrics[0].Metrics[0]
	assert.Equal(t, "pump.watch_stream_failures", metric.Name)

	// Verify the data points
	data, ok := metric.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	assert.Len(t, data.DataPoints, 1)
	assert.Equal(t, int64(2), data.DataPoints[0].Value)
}

func TestMetricsIntegration(t *testing.T) {
	t.Parallel()

	// This test verifies that the metrics instruments record into the reader
	// when driven through the same sequence the pump performs.
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	pm := newPumpMetrics(provider.Meter(meterName))

	// Simulate pump state transitions
	ctx := t.Context()
	pm.recordStateTransition(ctx, PumpStateWaitingForLock)
	pm.recordStateTransition(ctx, PumpStateAcquiring)
	pm.recordStateTransition(ctx, PumpStateWatching)
	pm.recordLockLossEvent(ctx)
	pm.recordStateTransition(ctx, PumpStateLockLost)
	pm.recordWatchStreamFailure(ctx)
	pm.recordStateTransition(ctx, PumpStateAcquiring)
	pm.recordStateTransition(ctx, PumpStateWatching)

	// Collect metrics
	rm := &metricdata.ResourceMetrics{}
	err := reader.Collect(ctx, rm)
	require.NoError(t, err)

	// Verify all metrics were recorded
	assert.Len(t, rm.ScopeMetrics, 1)
	assert.Len(t, rm.ScopeMetrics[0].Metrics, 3)

	// Find each metric by name
	metricsByName := make(map[string]metricdata.Metrics)
	for _, m := range rm.ScopeMetrics[0].Metrics {
		metricsByName[m.Name] = m
	}

	// Verify state transitions
	stateMetric, ok := metricsByName["pump.state_transitions"]
	require.True(t, ok)
	stateData, ok := stateMetric.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	// We have 4 unique states, so 4 data points (aggregated by state label)
	assert.Len(t, stateData.DataPoints, 4)

	// Verify the counts per state
	stateCounts := make(map[string]int64)
	for _, dp := range stateData.DataPoints {
		stateAttr, _ := dp.Attributes.Value(attribute.Key("state"))
		stateCounts[stateAttr.AsString()] = dp.Value
	}
	assert.Equal(t, int64(1), stateCounts["WaitingForLock"])
	assert.Equal(t, int64(2), stateCounts["Acquiring"])
	assert.Equal(t, int64(2), stateCounts["Watching"])
	assert.Equal(t, int64(1), stateCounts["LockLost"])

	// Verify lock loss events
	lockLossMetric, ok := metricsByName["pump.lock_loss_events"]
	require.True(t, ok)
	lockLossData, ok := lockLossMetric.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	assert.Equal(t, int64(1), lockLossData.DataPoints[0].Value)

	// Verify watch stream failures
	watchMetric, ok := metricsByName["pump.watch_stream_failures"]
	require.True(t, ok)
	watchData, ok := watchMetric.Data.(metricdata.Sum[int64])
	require.True(t, ok)
	assert.Equal(t, int64(1), watchData.DataPoints[0].Value)
}
