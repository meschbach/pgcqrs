package systest

import (
	"context"
	"os"
	"testing"

	"github.com/go-faker/faker/v4"
	"github.com/meschbach/pgcqrs/internal/junk"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
)

type harness struct {
	ctx    context.Context
	done   func()
	system *v1.System
	stream *v1.Stream
}

func setupHarnessT(t *testing.T) *harness {
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
	stream, err := system.Stream(ctx, appBase+"-"+faker.Name(), faker.Name())
	junk.Must(err)

	out := &harness{
		ctx:    ctx,
		done:   done,
		system: system,
		stream: stream,
	}
	t.Cleanup(done)
	return out
}

type matchedPair[T any] struct {
	envelope v1.Envelope
	entity   T
}
