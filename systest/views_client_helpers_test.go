package systest

import (
	"context"
	"testing"
	"time"

	"github.com/meschbach/pgcqrs/pkg/indexer"
	"github.com/meschbach/pgcqrs/pkg/indexer/views"
	"github.com/stretchr/testify/require"
)

// waitForState waits for the client to reach the target state
func waitForState(t *testing.T, client views.ProjectionClient, target indexer.PumpState) {
	t.Helper()
	require.NoError(t, client.WaitForState(t.Context(), target))
}

// closeClient closes the client, failing the test on error. Used via
// `t.Cleanup(func() { closeClient(t, client) })` so clients are released even
// when the test fails early.
func closeClient(t *testing.T, client views.ProjectionClient) {
	t.Helper()
	require.NoError(t, client.Close())
}

// waitForPositiveVersion polls client.Version until it reports a positive value.
// The pump flushes consumer_positions via heartbeat after the KV write, so a
// freshly-applied entity can briefly report version 0 on gRPC transports.
func waitForPositiveVersion(ctx context.Context, t *testing.T, client views.ProjectionClient, timeout ...time.Duration) int64 {
	t.Helper()
	actualWaitDuration := 3 * time.Second
	if len(timeout) > 0 {
		actualWaitDuration = timeout[0]
	}
	deadline := time.Now().Add(actualWaitDuration)
	for {
		version, err := client.Version(ctx)
		require.NoError(t, err)
		if version > 0 {
			return version
		}
		if time.Now().After(deadline) {
			require.FailNow(t, "projection version never became positive")
		}
		time.Sleep(25 * time.Millisecond)
	}
}
