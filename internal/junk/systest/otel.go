// Package systest provides system testing utilities.
package systest

import (
	"context"
	"os/signal"
	"time"

	"github.com/meschbach/go-junk-bucket/pkg/observability"
	"go.opentelemetry.io/otel"
	"golang.org/x/sys/unix"
)

var tracer = otel.Tracer("pgcqrs.systest")

// ApplicationDoneFunc is a function that cleans up the application context.
type ApplicationDoneFunc func()

// TraceApplication sets up tracing for an application and returns a context and a cleanup function.
func TraceApplication(name string, timeout time.Duration) (context.Context, ApplicationDoneFunc) {
	//nolint:forbidigo
	procCtx, procDone := signal.NotifyContext(context.Background(), unix.SIGTERM, unix.SIGINT, unix.SIGSTOP)
	cfg := observability.DefaultConfig(name)
	component, err := cfg.Start(procCtx)
	if err != nil {
		procDone()
		panic(err)
	}

	timedCtx, timedDone := context.WithTimeout(procCtx, timeout)
	ctx, span := tracer.Start(timedCtx, name)
	return ctx, func() {
		span.End()
		timedDone()
		//nolint:forbidigo
		shutdownCtx, shutdownDone := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownDone()
		if err := component.ShutdownGracefully(shutdownCtx); err != nil {
			panic(err)
		}
		procDone()
	}
}
