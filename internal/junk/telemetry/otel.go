package telemetry

import (
	"context"
	"fmt"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.10.0"
	"log"
	"os"
)

func newJaegerExporter(url string) (trace.SpanExporter, error) {
	// Create the Jaeger exporter
	return jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(url)))
}

// newResource returns a resource describing this application.
func newResource(cfg Config) *resource.Resource {
	envName := environmentFromEnv()
	//TODO: properly resolve the version and environment
	r, problem := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(cfg.ServiceName+"."+envName),
			semconv.ServiceVersionKey.String("v0.1.0"),
			attribute.String("environment", envName),
		),
	)
	if problem != nil {
		panic(problem)
	}
	return r
}

func environmentFromEnv() string {
	if envName, ok := os.LookupEnv("ENV"); ok {
		return envName
	} else {
		return "dev"
	}
}

func SetupTracing(programContext context.Context, config Config) error {
	//not sure why this manually needs to be configured
	otel.SetTextMapPropagator(propagation.TraceContext{})

	fmt.Printf("Tracing %#v\n", config)
	var exp trace.SpanExporter
	var err error
	switch config.Exporter {
	case "jaeger":
		jaegerEndpoint := envOrDefault("JAEGER_ENDPOINT", "http://localhost:14268/api/traces")
		fmt.Printf("Using %q for Jaeger endpoint\n", jaegerEndpoint)
		exp, err = newJaegerExporter(jaegerEndpoint)
	case "none":
	default:
		panic("Unknown exporter type " + config.Exporter)
	}
	if err != nil {
		return err
	}

	tp := trace.NewTracerProvider(
		trace.WithBatcher(exp),
		trace.WithResource(newResource(config)),
	)
	go func() {
		<-programContext.Done()
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Fatal(err)
		}
	}()
	otel.SetTracerProvider(tp)
	return nil
}
