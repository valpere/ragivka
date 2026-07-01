# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build both binaries
go build -v ./...

# Run all tests with race detection and coverage
go test -v -race -coverprofile=coverage.txt -covermode=atomic ./...

# Run a single package's tests
go test -v -race ./pkg/obs/...

# Run a single test by name
go test -v -run TestCalculateCost ./pkg/obs/...

# Lint
golangci-lint run

# Security scan
gosec ./...
govulncheck ./...

# Start infrastructure (PostgreSQL + pgvector + Redis)
docker compose -f docker/docker-compose.yml up -d

# Run API server
go run ./cmd/server/

# Run background worker
go run ./cmd/worker/
```

## Architecture

Ragivka is a multi-tenant RAG + workflow orchestration framework built as a statically compiled Go binary. It can run as a single process or split into independent API and Worker processes.

### Seven-layer architecture

```
Interfaces (Telegram, Web Widget)
  → App API (Auth/Tenants, Session Manager, Conversations)
    → Orchestrator (FSM, River Queue, Graph Engine, HITL Gates)
      → AI Layer (Model Router, Prompt Registry, Structured Output, Guardrails)
      → RAG Layer (Ingestion, Chunker/Embedder, Hybrid Retrieval, Re-ranker)
      → Tool Layer (Read / Draft / Write Tools + Audit)
        → Data Layer (PostgreSQL/pgvector, optional Redis, S3)
```

### Two entrypoints

- `cmd/server/` — HTTP server (port `$PORT`, default 8080). Exposes `/health`, `/metrics`; orchestrator/channel routes are registered but return 503 until dependency wiring lands (see below).
- `cmd/worker/` — Background River worker (metrics on port `$METRICS_PORT`, default 8081). Fully wired: repositories, `aicore`, retriever, and the River worker pool.

### Current implementation state

**Phase 1a — Observability scaffold (complete):**
- `pkg/obs/` — OpenTelemetry tracing (`trace.go`), Prometheus metrics (`metrics.go`), per-request cost accounting (`cost.go`). Use `obs.InitTracer`, `obs.LogRequestCost`, `obs.RecordRetrievalLatency`, etc.
- `pkg/db/` — `pgxpool` connection factory (`db.NewPool`). Uses `url.URL` construction to prevent DSN injection from special characters in credentials.
- `pkg/tenant/` — Tenant context carrier. `tenant.WithTenantID` injects into context; `tenant.GetTenantID` / `tenant.MustGetTenantID` extracts. Every repository function must call one of these before querying.

**Phase 1b — Session & Job Store (complete):**
- `migrations/` — goose SQL migrations, run idempotently on startup via `runtime.RunMigrations`.
- `pkg/runtime/` — Session FSM (`fsm.go`), `SessionRepository`/`MessageRepository` (Postgres-backed), River job registration (`jobs.go`, `tool_job.go`, `artifact_job.go`), `worker.go` (River client wiring).

**Phase 1c — AI Layer (complete):**
- `pkg/aicore/` — `ModelRouter` (`router.go`), Ollama client (`ollama.go`), Prompt Registry (`registry.go`), structured-output parsing (`structured.go`), input sanitization (`sanitize.go`).

**Phase 2 — RAG Pipeline (complete):**
- `pkg/knowledge/ingestion/` — Connector → Parser → Chunker (PII stripping) → Embedder → pgvector Indexer pipeline.
- `pkg/knowledge/retrieval/` — hybrid retriever (vector + keyword blend) + re-ranker.
- `pkg/tools/generators/` — deterministic PDF/Excel artifact generation from LLM-provided typed structs.

**Phase 3 — Orchestration + Tools (complete):**
- `pkg/tools/` — Tool Registry (Read/Draft/Write kind boundary), `AuditLogger` (idempotency + SHA-256 audit trail), `HITLGate`.
- `pkg/orchestrator/` — `TieredOrchestrator` dispatching L0 (direct), L1 (sync RAG + structured output + HITL gate), L2/L3 (async River job) handlers; `NewMessageHandler` for `POST /v1/sessions/{id}/messages`.
- `pkg/graph/` — DAG engine for L3 multi-agent workflows (`GraphEngine.Execute`, loop-back guard via `MaxIterations`, `NodeRegistry` for JSON-serialisable `GraphDef`).

**Phase 4 — Channel Adapters + MVP (complete):**
- `pkg/middleware/` — JWT auth (`JWTAuth`, NFR-23), Redis fixed-window rate limiter (`RateLimit`, FR-24/NFR-20), request-ID injection, standardized error envelope (`WriteError`, NFR-21), Telegram webhook secret validation.
- `pkg/channel/web/` — `POST /v1/sessions`, `GET /v1/sessions/{id}/messages`, `GET /ws/sessions/{id}` (WebSocket via `Broadcaster`; `MemoryBroadcaster` is single-process only — a Redis pub/sub-backed implementation is required once API and Worker run as separate processes).
- `pkg/channel/telegram/` — `POST /telegram/webhook/{tenantID}` webhook handler; resolves/creates a `USER` row via `runtime.UserRepository.ResolveOrCreate` (tenant-scoped, keyed by `channel_type`+`channel_id`) and replies synchronously for L0/L1 tiers. L2/L3 async replies are not yet delivered back to Telegram (would require a completion callback from the async worker).
- `pkg/runtime/user_repo.go` — `UserRepository.ResolveOrCreate` upserts into `USER`; required because `session.user_id` has a composite FK to `USER(tenant_id, id)` (migrations/001_foundation.sql) — sessions cannot be created for a user that doesn't have a `USER` row.
- L1Handler now requests structured JSON output (`{answer, confidence, requires_human}`, FR-15) from the model and evaluates it against `HITLGate` (threshold via `HITL_CONFIDENCE_THRESHOLD`, default 0.7). Below threshold, `requires_human=true`, or an unparseable response all escalate the session to `WaitingForHuman` instead of replying (fail-safe).
- `cmd/server/main.go` is now **fully wired**: repositories, `aicore` router, hybrid retriever, `HITLGate`, an insert-only River client (`riverEnqueuer`) for L2/L3 job enqueueing, Redis-backed rate limiting, JWT auth, and both channel adapters are constructed and routed. `cmd/worker/main.go` remains the process that actually registers and runs River `Workers`.
- `migrations/006_seed_mvp.sql` — fixture tenant/user/3 FAQ documents (chunks have `NULL` embeddings; hybrid retrieval falls back to keyword-only scoring for them) for `integration/l1_customer_support_test.go` and `scripts/smoke_test.sh`.
- See `README.md` → "Running the MVP" for the full local setup.

### Critical invariants

**Tenant isolation (NFR-16):** Every DB query must carry `tenant_id`. The canonical pattern:
```go
tenantID := tenant.MustGetTenantID(ctx)
// use tenantID in all queries
```
Cross-tenant queries are an architectural violation regardless of intent.

**Transaction boundaries (NFR-7):** Database transactions must not be held open during external API calls. The River job pattern is: short txn to claim → external work outside any txn → short txn to mark complete.

**Write Tool idempotency (NFR-4/NFR-15):** Any function that mutates external state must use an idempotency key and write an `AUDIT_LOG` record. The key must be stored and checked before re-executing.

### Observability wiring

Both `cmd/server` and `cmd/worker` call `obs.InitTracer` on startup. If `OTEL_EXPORTER_OTLP_ENDPOINT` is empty, a no-op tracer is used (safe for local dev). TLS is on by default; set `OTEL_EXPORTER_OTLP_INSECURE=true` only for local collectors.

`obs.LogRequestCost` records Prometheus counters and writes a structured JSON cost log to stdout. It falls back to gpt-4o-mini rates for unregistered models.

### Data model overview

All entities are scoped by `tenant_id`. Core tables: `TENANT`, `USER`, `SESSION` (FSM container with `version` for optimistic locking), `MESSAGE`, `RIVER_JOB`, `DOCUMENT`, `CHUNK` (pgvector + tsvector), `PROMPT_VERSION`, `ARTIFACT`, `AUDIT_LOG`. See `docs/architecture.md` for the full ER diagram and FSM state transitions.

### FSM states

`Active` → `WaitingForHuman` (low confidence / Write Tool requires approval) → `Active` or `Completed`  
`Active` → `Completed` | `Expired` (inactivity timeout)

### Requirement tracing

Source requirements are in `docs/requirements.md`. Code comments reference them as `// NFR-N` or `// FR-N`. When implementing new functionality, add the corresponding requirement tag.
