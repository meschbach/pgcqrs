package systest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGRPCPositionOperations(t *testing.T) {
	t.Parallel()
	skipUnlessGRPC(t)

	t.Run("ConsumerPositionCRUD", func(t *testing.T) {
		t.Parallel()

		harness := setupHarnessT(t)
		ctx := harness.ctx

		// Test SetPosition
		_, err := harness.transport.SetPosition(ctx, harness.appName, harness.streamName, "test-consumer", 123)
		require.NoError(t, err)

		// Test GetPosition
		pos, found, err := harness.transport.GetPosition(ctx, harness.appName, harness.streamName, "test-consumer")
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, int64(123), pos)

		// Test GetPosition when not set
		pos, found, err = harness.transport.GetPosition(ctx, harness.appName, harness.streamName, "non-existent-consumer")
		require.NoError(t, err)
		require.False(t, found)
		require.Equal(t, int64(0), pos)

		// Test ListConsumers
		_, err = harness.transport.SetPosition(ctx, harness.appName, harness.streamName, "consumer-a", 100)
		require.NoError(t, err)
		_, err = harness.transport.SetPosition(ctx, harness.appName, harness.streamName, "consumer-b", 200)
		require.NoError(t, err)

		consumers, err := harness.transport.ListConsumers(ctx, harness.appName, harness.streamName)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"test-consumer", "consumer-a", "consumer-b"}, consumers)

		// Test DeletePosition
		err = harness.transport.DeletePosition(ctx, harness.appName, harness.streamName, "consumer-a")
		require.NoError(t, err)

		pos, found, err = harness.transport.GetPosition(ctx, harness.appName, harness.streamName, "consumer-a")
		require.NoError(t, err)
		require.False(t, found)
		require.Equal(t, int64(0), pos)

		// Verify consumer-b still exists
		pos, found, err = harness.transport.GetPosition(ctx, harness.appName, harness.streamName, "consumer-b")
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, int64(200), pos)
	})
}
