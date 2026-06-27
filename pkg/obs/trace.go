package obs

import (
	"context"
	"fmt"
	"log"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

var globalTracer trace.Tracer

// InitTracer initializes OpenTelemetry distributed tracing.
// It sets up the OTLP HTTP exporter pointing to endpoint (e.g. "localhost:4318").
// If endpoint is empty, it returns a no-op shutdown function and sets up a local-only tracer provider.
// NFR-11: distributed tracing configuration across services.
func InitTracer(ctx context.Context, serviceName string, endpoint string) (func(context.Context) error, error) {
	var exporter sdktrace.SpanExporter
	var err error

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	if endpoint != "" {
		log.Printf("Initializing OTLP HTTP trace exporter to %s", endpoint)
		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpoint(endpoint),
		}
		// Default to secure in production unless explicitly overridden via env var
		if os.Getenv("OTEL_EXPORTER_OTLP_INSECURE") == "true" {
			log.Println("WARNING: OTLP trace exporter is using insecure connection")
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		exporter, err = otlptracehttp.New(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
		}
	}

	var bsp sdktrace.SpanProcessor
	if exporter != nil {
		bsp = sdktrace.NewBatchSpanProcessor(exporter)
	}

	tpOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
	}
	if bsp != nil {
		tpOpts = append(tpOpts, sdktrace.WithSpanProcessor(bsp))
	}

	tp := sdktrace.NewTracerProvider(tpOpts...)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	globalTracer = tp.Tracer(serviceName)

	shutdown := func(shutdownCtx context.Context) error {
		if err := tp.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("failed to shutdown TracerProvider: %w", err)
		}
		return nil
	}

	return shutdown, nil
}

// GetTracer returns the global tracer for instrumentation.
func GetTracer() trace.Tracer {
	if globalTracer == nil {
		return otel.Tracer("ragivka-fallback")
	}
	return globalTracer
}

// StartSpan starts a new tracing span from the context.
// NFR-11: trace spanning standard utility.
func StartSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	return GetTracer().Start(ctx, name)
}
