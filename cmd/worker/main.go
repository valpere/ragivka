package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/valpere/ragivka/pkg/obs"
)

// NFR-9: Deployment Modes (Worker Mode)
func main() {
	log.Println("Starting Ragivka Background Worker Mode...")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1. Setup OpenTelemetry
	// NFR-11: distributed tracing
	otelEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	shutdownTracer, err := obs.InitTracer(ctx, "ragivka-background-worker", otelEndpoint)
	if err != nil {
		log.Fatalf("failed to initialize tracing: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracer(shutdownCtx); err != nil {
			log.Printf("error shutting down tracer: %v", err)
		}
	}()

	// 2. Expose Prometheus metrics endpoint
	// NFR-12 Metrics
	mux := http.NewServeMux()
	mux.Handle("/metrics", obs.MetricsHandler())

	port := os.Getenv("METRICS_PORT")
	if port == "" {
		port = "8081" // default metrics port for worker
	}

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errChan := make(chan error, 1)
	go func() {
		log.Printf("Worker metrics listening on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
	}()

	// 3. Start River worker pool (stubbed for Phase 1)
	// Placeholder for River worker start

	var startupErr error
	select {
	case <-ctx.Done():
		log.Println("Shutting down Background Worker gracefully...")
		// Non-blocking drain check to see if an error was simultaneously received
		select {
		case err := <-errChan:
			startupErr = err
		default:
		}
	case err := <-errChan:
		log.Printf("Worker metrics server error: %v. Initiating shutdown...", err)
		startupErr = err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("error during metrics server shutdown: %v", err)
	}
	log.Println("Worker stopped")

	if startupErr != nil {
		os.Exit(1)
	}
}
