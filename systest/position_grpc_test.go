package systest

import (
	"context"
	"os"
	"testing"

	"github.com/go-faker/faker/v4"
	"github.com/meschbach/pgcqrs/internal/junk"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/require"
)

func TestGRPCPositionOperations(t *testing.T) {
	if os.Getenv("PGCQRS_TEST_TRANSPORT") != "grpc" {
		t.Skip("Skipping gRPC integration test - PGCQRS_TEST_TRANSPORT not set to grpc")
	}

	t.Parallel()

	t.Run("ConsumerPositionCRUD", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		harness := setupPositionHarnessT(t)
		defer harness.done()

		// Test SetPosition
		_, err := harness.system.Transport.SetPosition(ctx, harness.appName, harness.streamName, "test-consumer", 123)
		require.NoError(t, err)

		// Test GetPosition
		pos, found, err := harness.system.Transport.GetPosition(ctx, harness.appName, harness.streamName, "test-consumer")
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, int64(123), pos)

		// Test GetPosition when not set
		pos, found, err = harness.system.Transport.GetPosition(ctx, harness.appName, harness.streamName, "non-existent-consumer")
		require.NoError(t, err)
		require.False(t, found)
		require.Equal(t, int64(0), pos)

		// Test ListConsumers
		_, err = harness.system.Transport.SetPosition(ctx, harness.appName, harness.streamName, "consumer-a", 100)
		require.NoError(t, err)
		_, err = harness.system.Transport.SetPosition(ctx, harness.appName, harness.streamName, "consumer-b", 200)
		require.NoError(t, err)

		consumers, err := harness.system.Transport.ListConsumers(ctx, harness.appName, harness.streamName)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"consumer-a", "consumer-b"}, consumers)

		// Test DeletePosition
		err = harness.system.Transport.DeletePosition(ctx, harness.appName, harness.streamName, "consumer-a")
		require.NoError(t, err)

		pos, found, err = harness.system.Transport.GetPosition(ctx, harness.appName, harness.streamName, "consumer-a")
		require.NoError(t, err)
		require.False(t, found)
		require.Equal(t, int64(0), pos)

		// Verify consumer-b still exists
		pos, found, err = harness.system.Transport.GetPosition(ctx, harness.appName, harness.streamName, "consumer-b")
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, int64(200), pos)
	})
}

type positionHarness struct {
	ctx        context.Context
	done       func()
	system     *v1.System
	appName    string
	streamName string
}

func setupPositionHarnessT(t *testing.T) *positionHarness {
	ctx, done := context.WithCancel(t.Context())
	transport, hasTransport := os.LookupEnv("PGCQRS_TEST_TRANSPORT")
	url, hasURL := os.LookupEnv("PGCQRS_TEST_URL")
	appBase, hasAppBase := os.LookupEnv("PGCQRS_TEST_APP_BASE")

	if !hasURL || !hasAppBase {
		panic("Requires env PGCQRS_TEST_URL and PGCQRS_TEST_APP_BASE but is missing at least one")
	}

	if !hasTransport {
		transport = v1.TransportTypeHTTP
	}

	config := v1.Config{
		TransportType: transport,
		ServiceURL:    url,
	}
	system, err := config.SystemFromConfig()
	junk.Must(err)
	_, err = system.Stream(ctx, appBase+"-"+faker.Name(), faker.Name())
	junk.Must(err)

	out := &positionHarness{
		ctx:        ctx,
		done:       done,
		system:     system,
		appName:    appBase + "-" + faker.Name(),
		streamName: faker.Name(),
	}
	t.Cleanup(done)
	return out
}
