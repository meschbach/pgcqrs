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

func buildSoftTimeoutContext(base context.Context) (context.Context, func(), error) {
	spec, has := os.LookupEnv("SOFT_TIMEOUT")
	if !has {
		return base, func() {}, nil
	}
	length, err := time.ParseDuration(spec)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(base, length)
	return ctx, cancel, nil
}

func setupHarnessT(t *testing.T) (*harness, context.Context, *v1.System) {
	ctx, done, ctxErr := buildSoftTimeoutContext(t.Context())
	require.NoError(t, ctxErr)
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
		ctx:    ctx,
		done:   done,
		system: system,
	}

	cfg := observability.DefaultConfig("pgcqrs:systest")
	component, err := cfg.Start(h.ctx)
	require.NoError(t, err, "observability error")

	t.Cleanup(func() {
		require.NoError(t, component.ShutdownGracefully(t.Context()))
		h.done()
	})
	return h, h.ctx, h.system
}
