package views

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// TestMemoryClientWiring pins the invariants that keep the memory backend
// coherent: the reader must read from the same store the indexer writes to,
// and it must wait on the same notifier the indexer emits to. Splitting either
// pair causes stale reads or UntilVersion waits that never observe emissions.
func TestMemoryClientWiring(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)

	sys := v1.NewSystem(transport)
	proj := NewProjection("domain", "stream")

	client, err := With(ctx, sys, proj)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	memClient, ok := client.(*Client[*v1.MemoryLock])
	require.True(t, ok)

	store, ok := memClient.store.(*MemoryStore)
	require.True(t, ok, "store must be a *MemoryStore")
	require.NotNil(t, store)

	reader, ok := memClient.reader.(*MemoryReader)
	require.True(t, ok, "reader must be a *MemoryReader")
	require.NotNil(t, reader)

	assert.Same(t, store, reader.store, "reader and indexer must share one store")
	assert.Same(t, memClient.notifier, reader.notifier, "reader must wait on the notifier the indexer emits to")
	require.NotNil(t, memClient.pump)
}

// TestMemoryClientEndToEnd exercises newMemoryClient directly through the full
// event flow: submit, project, read with a version constraint, and version.
func TestMemoryClientEndToEnd(t *testing.T) {
	t.Parallel()
	transport := v1.NewMemoryTransport()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)

	sys := v1.NewSystem(transport)
	stream, err := sys.Stream(ctx, "domain", "stream")
	require.NoError(t, err)

	proj := NewProjection("domain", "stream",
		OnKind("TestEvent", func(_ context.Context, _ v1.Envelope, evt *testEvent, _ *ReduceContext) (*ReduceResult, error) {
			return &ReduceResult{
				Upserts: []Upsert{
					{Kind: "items", Key: NewKey("item-1"), Value: evt},
				},
			}, nil
		}),
	)

	client, err := newMemoryClient(ctx, transport, proj, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	sub, err := stream.Submit(ctx, "TestEvent", testEvent{ID: "event-0", Name: "Widget"})
	require.NoError(t, err)

	entity, result, err := client.Get(ctx, "items", NewKey("item-1"), UntilVersion(sub.ID, 5*time.Second))
	require.NoError(t, err)
	require.Equal(t, StatusOK, result.Status)
	require.NotNil(t, entity)
	var projected testEvent
	require.NoError(t, json.Unmarshal(entity.Value, &projected))
	assert.Equal(t, "event-0", projected.ID)
	assert.Equal(t, "Widget", projected.Name)

	version, err := client.Version(ctx)
	require.NoError(t, err)
	assert.Equal(t, sub.ID, version)
}

// TestClassifyConnectivity verifies the backend dispatch is explicit and
// exhaustive: memory connectivity selects the memory backend, a gRPC connection
// selects the gRPC backend, and connectivity exposing neither fails loudly.
func TestClassifyConnectivity(t *testing.T) {
	t.Parallel()

	transport := v1.NewMemoryTransport()
	vf, ok := transport.(v1.ViewFeature)
	require.True(t, ok, "memory transport must implement ViewFeature")

	backend, err := classifyConnectivity(vf.ViewConnectivity())
	require.NoError(t, err)
	assert.Equal(t, backendMemory, backend)

	backend, err = classifyConnectivity(v1.ViewConnectivity{GRPC: &grpc.ClientConn{}})
	require.NoError(t, err)
	assert.Equal(t, backendGRPC, backend)

	_, err = classifyConnectivity(v1.ViewConnectivity{})
	require.Error(t, err)
}
