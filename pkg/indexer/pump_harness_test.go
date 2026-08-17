package indexer

import (
	"context"
	"testing"
	"time"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/require"
)

// newMemoryPumpHarness wires an in-memory transport, stream, and pump together
// so tests can exercise the pump without external dependencies.
func newMemoryPumpHarness(t *testing.T, domain, stream string, opts ...Option) (v1.Transport, *v1.Stream, *Pump[*v1.MemoryLock]) {
	t.Helper()
	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	s, err := sys.Stream(t.Context(), domain, stream)
	require.NoError(t, err)
	pump := NewPump(v1.NewMemoryWire(transport), &mockIndexer{stream: s}, "test-holder", opts...)
	return transport, s, pump
}

// newRecordingPumpHarness wires an in-memory transport, stream, and pump together
// with a recordingIndexer that tracks which events were processed. Use this for
// tests that need to verify event delivery and processing behavior.
func newRecordingPumpHarness(t *testing.T, domain, stream string, opts ...Option) (v1.Transport, *v1.Stream, *Pump[*v1.MemoryLock], *recordingIndexer) {
	t.Helper()
	transport := v1.NewMemoryTransport()
	sys := v1.NewSystem(transport)
	s, err := sys.Stream(t.Context(), domain, stream)
	require.NoError(t, err)
	indexer := &recordingIndexer{stream: s}
	pump := NewPump(v1.NewMemoryWire(transport), indexer, "test-holder", opts...)
	return transport, s, pump, indexer
}

// runPump runs the pump for the given duration and returns its error.
func runPump(t *testing.T, pump *Pump[*v1.MemoryLock], domain, stream string, timeout time.Duration) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	return pump.RunWithDomainStream(ctx, domain, stream)
}
