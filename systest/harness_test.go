package systest

import (
	"context"
	"os"
	"testing"

	"github.com/meschbach/pgcqrs/pkg/junk/faking"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/require"
)

var domains = faking.NewUniqueKebab()

type harness struct {
	ctx        context.Context
	done       func()
	system     *v1.System
	stream     *v1.Stream
	transport  v1.Transport
	appName    string
	streamName string
	serviceURL string
}

func setupHarnessT(t *testing.T) *harness {
	t.Helper()
	ctx, done := context.WithCancel(t.Context())

	transportType := os.Getenv("PGCQRS_TEST_TRANSPORT")
	url := os.Getenv("PGCQRS_TEST_URL")
	appBase := os.Getenv("PGCQRS_TEST_APP_BASE")

	require.NotEmpty(t, url, "Requires env PGCQRS_TEST_URL")
	require.NotEmpty(t, appBase, "Requires env PGCQRS_TEST_APP_BASE")

	if transportType == "" {
		transportType = v1.TransportTypeHTTP
	}

	appName := appBase + "-" + domains.Next()
	streamName := domains.Next()

	config := v1.Config{
		TransportType: transportType,
		ServiceURL:    url,
	}
	system, err := config.SystemFromConfig()
	require.NoError(t, err)

	stream, err := system.Stream(ctx, appName, streamName)
	require.NoError(t, err)

	h := &harness{
		ctx:        ctx,
		done:       done,
		system:     system,
		stream:     stream,
		transport:  system.Transport,
		appName:    appName,
		streamName: streamName,
		serviceURL: url,
	}
	t.Cleanup(done)
	return h
}

func skipHTTP(t *testing.T) {
	t.Helper()
	transport := os.Getenv("PGCQRS_TEST_TRANSPORT")
	if transport == "http" || transport == "" {
		t.Skip("Skipping test - HTTP transport not supported")
	}
}

func skipUnlessGRPC(t *testing.T) {
	t.Helper()
	if os.Getenv("PGCQRS_TEST_TRANSPORT") != "grpc" {
		t.Skip("Skipping gRPC integration test - PGCQRS_TEST_TRANSPORT not set to grpc")
	}
}

type matchedPair[T any] struct {
	envelope v1.Envelope
	entity   T
}
