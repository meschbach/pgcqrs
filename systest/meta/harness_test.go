package meta

import (
	"context"
	"github.com/meschbach/pgcqrs/internal/junk"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"os"
	"testing"
)

type harness struct {
	ctx    context.Context
	done   func()
	system *v1.System
}

func setupHarness() *harness {
	ctx, done := context.WithCancel(context.Background())
	transport, hasTransport := os.LookupEnv("PGCQRS_TEST_TRANSPORT")
	url, hasURL := os.LookupEnv("PGCQRS_TEST_URL")

	if !hasURL {
		panic("Requires env PGCQRS_TEST_URL but is missing at least one")
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

	out := &harness{
		ctx:    ctx,
		done:   done,
		system: system,
	}
	return out
}

func setupHarnessT(t *testing.T) (*harness, context.Context, *v1.System) {
	h := setupHarness()
	t.Cleanup(func() {
		h.done()
	})
	return h, h.ctx, h.system
}
