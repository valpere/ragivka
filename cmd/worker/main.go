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

	"github.com/valpere/ragivka/pkg/aicore"
	"github.com/valpere/ragivka/pkg/db"
	"github.com/valpere/ragivka/pkg/knowledge/ingestion"
	"github.com/valpere/ragivka/pkg/obs"
	"github.com/valpere/ragivka/pkg/runtime"
	"github.com/valpere/ragivka/pkg/storage"
)

// NFR-9: Deployment Modes (Worker Mode)
func main() {
	log.Println("Starting Ragivka Background Worker Mode...")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1. Tracing (NFR-11)
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

	// 2. Database pool
	pool, err := db.NewPool(ctx, dbConfig())
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer pool.Close()

	// 3. Migrations — idempotent; worker runs them so it can start standalone (NFR-9)
	if err := runtime.RunMigrations(ctx, pool); err != nil {
		log.Fatalf("migrations failed: %v", err)
	}
	log.Println("Migrations applied")

	// 4. Repositories and aicore components (Phase 1c)
	sessions := runtime.NewSessionRepository(pool)
	messages := runtime.NewMessageRepository(pool)

	ollamaClient := aicore.NewOllamaClient(aicore.OllamaConfig{
		APIURL: getenv("OLLAMA_API_URL", "https://ollama.com/api/chat"),
		APIKey: os.Getenv("OLLAMA_API_KEY"),
		Model:  getenv("OLLAMA_MODEL", "qwen3.5:cloud"),
	})
	router := aicore.NewRouter(ollamaClient, aicore.DefaultPolicy())
	registry := aicore.NewPromptRegistry(pool)

	// 5. Ingestion pipeline (Phase 2b, FR-8, FR-9, NFR-18)
	s3Client := storage.NewS3Client(storage.S3Config{
		Bucket:          getenv("S3_BUCKET", "ragivka"),
		Region:          getenv("S3_REGION", "us-east-1"),
		Endpoint:        os.Getenv("S3_ENDPOINT"),
		AccessKeyID:     os.Getenv("S3_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("S3_SECRET_ACCESS_KEY"),
		UsePathStyle:    os.Getenv("S3_ENDPOINT") != "",
	})
	ingestPipeline := ingestion.NewPipeline(
		ingestion.NewS3Connector(s3Client),
		ingestion.NewRegexScrubber(),
		ingestion.NewOllamaEmbedder(ingestion.OllamaEmbedConfig{
			APIURL: getenv("OLLAMA_EMBED_URL", "https://ollama.com/api/embed"),
			APIKey: os.Getenv("OLLAMA_API_KEY"),
			Model:  getenv("OLLAMA_EMBED_MODEL", "bge-m3:latest"),
		}),
		ingestion.NewIndexer(pool),
		ingestion.NewDocumentRepository(pool),
		ingestion.DefaultChunkConfig(),
	)
	ingestWorker := ingestion.NewIngestDocumentWorker(ingestPipeline)

	// 6. Metrics endpoint (NFR-12)
	mux := http.NewServeMux()
	mux.Handle("/metrics", obs.MetricsHandler())

	metricsPort := getenv("METRICS_PORT", "8081")
	metricsServer := &http.Server{
		Addr:         ":" + metricsPort,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errChan := make(chan error, 1)
	go func() {
		log.Printf("Worker metrics listening on %s", metricsServer.Addr)
		if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
	}()

	// 7. River worker pool — blocks until ctx is cancelled, then drains gracefully (NFR-7)
	workerErrChan := make(chan error, 1)
	go func() {
		log.Println("Starting River worker pool")
		if err := runtime.StartWorker(ctx, pool, sessions, messages, router, registry, ingestWorker); err != nil {
			workerErrChan <- err
		}
	}()

	var startupErr error
	select {
	case <-ctx.Done():
		log.Println("Shutting down Background Worker gracefully...")
		select {
		case err := <-errChan:
			startupErr = err
		case err := <-workerErrChan:
			startupErr = err
		default:
		}
	case err := <-errChan:
		log.Printf("Metrics server error: %v", err)
		startupErr = err
		stop()
	case err := <-workerErrChan:
		log.Printf("River worker error: %v", err)
		startupErr = err
		stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("error during metrics server shutdown: %v", err)
	}
	log.Println("Worker stopped")

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
