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

- `cmd/server/` — HTTP server (port `$PORT`, default 8080). Exposes `/health` and `/metrics`.
- `cmd/worker/` — Background River worker (metrics on port `$METRICS_PORT`, default 8081). River integration is stubbed in Phase 1.

### Current implementation state

**Phase 1a — Observability scaffold (complete):**
- `pkg/obs/` — OpenTelemetry tracing (`trace.go`), Prometheus metrics (`metrics.go`), per-request cost accounting (`cost.go`). Use `obs.InitTracer`, `obs.LogRequestCost`, `obs.RecordRetrievalLatency`, etc.
- `pkg/db/` — `pgxpool` connection factory (`db.NewPool`). Uses `url.URL` construction to prevent DSN injection from special characters in credentials.
- `pkg/tenant/` — Tenant context carrier. `tenant.WithTenantID` injects into context; `tenant.GetTenantID` / `tenant.MustGetTenantID` extracts. Every repository function must call one of these before querying.
- `cmd/server/` — HTTP server skeleton with `/health` and `/metrics` only.
- `cmd/worker/` — metrics shell; River worker pool is **stubbed** (no real jobs registered).

**Phase 1b — Session & Job Store (not started):**
Missing before Phase 1b can begin: SQL migrations (`migrations/`), `pkg/runtime/` package (Session FSM, River integration, HITL gates), River job arg/result types.

**Phase 1c — AI Layer (not started):**
`pkg/aicore/` — Model Router, Prompt Registry, Structured Output.

**Phases 2–4 (not started):**
Planned package locations: `pkg/knowledge/`, `pkg/channel/`, `pkg/tools/`, `pkg/guardrails/`, `pkg/graph/`.

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
