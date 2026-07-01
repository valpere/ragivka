package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/valpere/ragivka/pkg/aicore"
	"github.com/valpere/ragivka/pkg/channel/telegram"
	"github.com/valpere/ragivka/pkg/channel/web"
	"github.com/valpere/ragivka/pkg/db"
	"github.com/valpere/ragivka/pkg/knowledge/ingestion"
	"github.com/valpere/ragivka/pkg/knowledge/retrieval"
	"github.com/valpere/ragivka/pkg/middleware"
	"github.com/valpere/ragivka/pkg/obs"
	"github.com/valpere/ragivka/pkg/orchestrator"
	"github.com/valpere/ragivka/pkg/runtime"
	"github.com/valpere/ragivka/pkg/tools"
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

	// 4. Repositories
	users := runtime.NewUserRepository(pool)
	sessions := runtime.NewSessionRepository(pool)
	messages := runtime.NewMessageRepository(pool)

	// 5. AI layer (Phase 1c) — shared by L1 generation and hybrid retrieval embedding.
	ollamaClient := aicore.NewOllamaClient(aicore.OllamaConfig{
		APIURL: getenv("OLLAMA_API_URL", "https://ollama.com/api/chat"),
		APIKey: os.Getenv("OLLAMA_API_KEY"),
		Model:  getenv("OLLAMA_MODEL", "qwen3.5:cloud"),
	})
	router := aicore.NewRouter(ollamaClient, aicore.DefaultPolicy())

	embedder := ingestion.NewOllamaEmbedder(ingestion.OllamaEmbedConfig{
		APIURL:      getenv("OLLAMA_EMBED_URL", "https://ollama.com/api/embed"),
		APIKey:      os.Getenv("OLLAMA_API_KEY"),
		Model:       getenv("OLLAMA_EMBED_MODEL", "bge-m3:latest"),
		ExpectedDim: 1024,
	})
	retriever := retrieval.NewRetriever(pool, embedder, retrieval.NewDotProductReranker())

	// 6. HITL gate (FR-18) — threshold configurable via HITL_CONFIDENCE_THRESHOLD (default 0.7).
	hitlGate := tools.NewHITLGate(hitlThreshold())

	// 7. River insert-only client (NFR-7) — the API server enqueues L2/L3 jobs;
	// cmd/worker owns the Workers registration and actually runs them.
	riverClient, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	if err != nil {
		log.Fatalf("river client: %v", err)
	}
	// Stop is a safe no-op for a client that never called Start (insert-only
	// usage here); deferred for symmetry with the other resources below.
	defer func() { _ = riverClient.Stop(context.Background()) }()
	enqueuer := &riverEnqueuer{client: riverClient}

	// 8. Orchestrator (FR-1/FR-2/FR-3)
	orch := orchestrator.NewTieredOrchestrator(
		sessions,
		orchestrator.NewL0Handler(router, sessions, messages),
		orchestrator.NewL1Handler(router, sessions, messages, retriever, hitlGate),
		orchestrator.NewL2Handler(sessions, messages, enqueuer),
	)

	// 9. Redis (rate limiting, FR-24/NFR-20)
	redisClient := redis.NewClient(&redis.Options{Addr: getenv("REDIS_ADDR", "localhost:6379")})
	defer func() { _ = redisClient.Close() }()
	rateLimiter := middleware.NewRedisRateLimiter(redisClient)
	rateLimitPerMin := envInt("RATE_LIMIT_REQUESTS_PER_MIN", 60)
	limitFor := func(string) int { return rateLimitPerMin }

	const devInsecureJWTSecret = "dev-insecure-secret-change-me" // #nosec G101 -- placeholder default, not a real credential
	jwtSecretRaw := getenv("JWT_SECRET", devInsecureJWTSecret)
	if jwtSecretRaw == devInsecureJWTSecret {
		log.Println("WARNING: JWT_SECRET is not set — using an insecure, publicly-known default. Set JWT_SECRET before exposing this server.")
	}
	jwtSecret := []byte(jwtSecretRaw)
	sessionTTL := time.Duration(envInt("SESSION_TTL_MINUTES", 30)) * time.Minute

	// 10. Channel adapters (FR-21, FR-22)
	broadcaster := web.NewMemoryBroadcaster()
	telegramSender := telegram.NewHTTPSender(os.Getenv("TELEGRAM_BOT_TOKEN"))
	telegramWebhookSecret := os.Getenv("TELEGRAM_WEBHOOK_SECRET")
	if telegramWebhookSecret == "" {
		log.Println("WARNING: TELEGRAM_WEBHOOK_SECRET is not set — the Telegram webhook route will reject all requests until it is configured.")
	}

	// 11. HTTP server (NFR-12)
	mux := http.NewServeMux()
	mux.Handle("/metrics", obs.MetricsHandler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	webChain := chainMiddleware(middleware.RequestID, middleware.JWTAuth(jwtSecret), middleware.RateLimit(rateLimiter, limitFor))
	// POST /v1/sessions/{id}/messages — calls TieredOrchestrator.Run (FR-1/FR-2/FR-3).
	mux.Handle("POST /v1/sessions/{id}/messages", webChain(orchestrator.NewMessageHandler(orch)))
	// GET /v1/sessions/{id}/messages — conversation history (FR-22).
	mux.Handle("GET /v1/sessions/{id}/messages", webChain(web.NewListMessagesHandler(sessions, messages)))
	// POST /v1/sessions — create a session for the web widget (FR-22).
	mux.Handle("POST /v1/sessions", webChain(web.NewCreateSessionHandler(sessions, sessionTTL)))
	// GET /ws/sessions/{id} — async reply delivery over WebSocket (FR-22).
	mux.Handle("GET /ws/sessions/{id}", webChain(web.NewWebSocketHandler(sessions, broadcaster)))

	telegramChain := chainMiddleware(middleware.RequestID, middleware.TelegramSecretAuth(telegramWebhookSecret))
	// POST /telegram/webhook/{tenantID} — Telegram Bot API webhook (FR-21).
	mux.Handle("POST /telegram/webhook/{tenantID}", telegramChain(
		telegram.NewWebhookHandler(users, sessions, messages, orch, telegramSender, sessionTTL),
	))

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

// riverEnqueuer adapts *river.Client to orchestrator.JobEnqueuer (NFR-7).
// The API server only inserts jobs; cmd/worker registers Workers and runs them.
type riverEnqueuer struct {
	client *river.Client[pgx.Tx]
}

func (e *riverEnqueuer) EnqueueGenerateResponse(ctx context.Context, args runtime.GenerateResponseArgs) error {
	_, err := e.client.Insert(ctx, args, nil)
	return err
}

// chainMiddleware composes middleware in the given order: the first
// argument runs outermost (first to see the request).
func chainMiddleware(mws ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(final http.Handler) http.Handler {
		h := final
		for i := len(mws) - 1; i >= 0; i-- {
			h = mws[i](h)
		}
		return h
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

// hitlThreshold reads HITL_CONFIDENCE_THRESHOLD (FR-18); defaults to 0.7.
func hitlThreshold() float64 {
	raw := os.Getenv("HITL_CONFIDENCE_THRESHOLD")
	if raw == "" {
		return 0.7
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		// raw is deployment-operator-configured env, not network input;
		// #nosec G706 -- not a log-injection vector.
		log.Printf("invalid HITL_CONFIDENCE_THRESHOLD %q, using default 0.7: %v", raw, err)
		return 0.7
	}
	return v
}

func envInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		// raw is deployment-operator-configured env, not network input;
		// #nosec G706 -- not a log-injection vector.
		log.Printf("invalid %s %q, using default %d: %v", key, raw, fallback, err)
		return fallback
	}
	return v
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
