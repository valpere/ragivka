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

	"github.com/valpere/ragivka/pkg/db"
	"github.com/valpere/ragivka/pkg/obs"
	"github.com/valpere/ragivka/pkg/runtime"
)

// NFR-9: Deployment Modes (API Server)
func main() {
	log.Println("Starting Ragivka API Server Mode...")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1. Tracing (NFR-11)
	otelEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	shutdownTracer, err := obs.InitTracer(ctx, "ragivka-api-server", otelEndpoint)
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

	// 2. Database pool
	pool, err := db.NewPool(ctx, dbConfig())
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer pool.Close()

	// 3. Migrations (goose + River) — idempotent, safe on every startup
	if err := runtime.RunMigrations(ctx, pool); err != nil {
		log.Fatalf("migrations failed: %v", err)
	}
	log.Println("Migrations applied")

	// 4. HTTP server with /health and /metrics (NFR-12)
	mux := http.NewServeMux()
	mux.Handle("/metrics", obs.MetricsHandler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	port := getenv("PORT", "8080")
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errChan := make(chan error, 1)
	go func() {
		log.Printf("API Server listening on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
	}()

	var startupErr error
	select {
	case <-ctx.Done():
		log.Println("Shutting down API Server gracefully...")
		select {
		case err := <-errChan:
			startupErr = err
		default:
		}
	case err := <-errChan:
		log.Printf("API Server error: %v. Initiating shutdown...", err)
		startupErr = err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("error during server shutdown: %v", err)
	}
	log.Println("Server stopped")

	if startupErr != nil {
		os.Exit(1)
	}
}

func dbConfig() db.Config {
	return db.Config{
		Host:     getenv("DB_HOST", "localhost"),
		Port:     getenv("DB_PORT", "5432"),
		User:     getenv("DB_USER", "ragivka"),
		Password: getenv("DB_PASSWORD", "ragivka_password"),
		Database: getenv("DB_NAME", "ragivka_db"),
		SSLMode:  getenv("DB_SSLMODE", "disable"),
		MaxConns: 20,
		MinConns: 2,
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
