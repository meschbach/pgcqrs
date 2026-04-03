package meta

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/meschbach/go-junk-bucket/pkg/observability"
	"github.com/meschbach/pgcqrs/internal/junk"
	v1 "github.com/meschbach/pgcqrs/pkg/v1"
	"github.com/stretchr/testify/require"
)

type harness struct {
	ctx    context.Context
	done   func()
	system *v1.System
}

func setupHarnessT(t *testing.T) (*harness, context.Context, *v1.System) {
	ctx := t.Context()
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

	h := &harness{
		ctx: ctx,
		done: func() {

		},
		system: system,
	}

	cfg := observability.DefaultConfig("pgcqrs:systest")
	component, err := cfg.Start(h.ctx)
	require.NoError(t, err, "observability error")

	t.Cleanup(func() {
		//nolint
		cleanup, done := context.WithTimeout(context.Background(), 500*time.Second)
		defer done()

		require.NoError(t, component.ShutdownGracefully(cleanup))
		h.done()
	})
	return h, h.ctx, h.system
}
