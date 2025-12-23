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

type ApplicationDoneFunc func()

func TraceApplication(name string, timeout time.Duration) (context.Context, ApplicationDoneFunc) {
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
		shutdownCtx, shutdownDone := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownDone()
		if err := component.ShutdownGracefully(shutdownCtx); err != nil {
			panic(err)
		}
		procDone()
	}
}
