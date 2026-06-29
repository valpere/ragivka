---
name: onboard
description: "Ragivka onboarding — architecture, commands, gotchas. Usage: /onboard"
---

# Skill: /onboard
# Ragivka — Onboarding Brief

---

```
PROJECT  : Ragivka
PURPOSE  : Multi-tenant RAG + workflow orchestration framework for Go —
           Telegram bots, web widgets, and AI pipelines for Ukrainian SMB clients.
BORN     : 2026-06-27
```

---

## STACK

```
Runtime    : Go 1.26
Framework  : none (stdlib + pgxpool + OpenTelemetry + Prometheus)
Database   : PostgreSQL 16 + pgvector (docker/docker-compose.yml)
             Redis 7 (optional — ephemeral only, never durable state)
Tests      : go test — 1 test file (pkg/obs/cost_test.go) — run with: go test -v -race ./...
CI         : GitHub Actions — .github/workflows/ci.yml
             Jobs: build · test+coverage · golangci-lint · gosec · govulncheck
```

---

## ARCHITECTURE

7-layer system (Interfaces → App API → Orchestrator → AI/RAG/Tool → Data).
The binary can run as API server, background worker, or both in the same process.

```
Entry      : cmd/server/main.go  (HTTP :8080, /health + /metrics)
             cmd/worker/main.go  (metrics :8081, River worker — stubbed in Phase 1)
Key pkgs   : pkg/obs/     — OpenTelemetry tracing, Prometheus metrics, cost accounting
             pkg/db/      — pgxpool connection factory (DSN-injection-safe)
             pkg/tenant/  — tenant context carrier (WithTenantID / MustGetTenantID)
Planned    : pkg/runtime/ pkg/aicore/ pkg/knowledge/ pkg/channel/ pkg/tools/
             pkg/guardrails/ pkg/graph/  ← Phases 2–4, not yet created
```

---

## HOW TO RUN

```bash
# Infrastructure
docker compose -f docker/docker-compose.yml up -d

# API server
go run ./cmd/server/        # PORT (default 8080)

# Background worker
go run ./cmd/worker/        # METRICS_PORT (default 8081)

# Test all
go test -v -race -coverprofile=coverage.txt -covermode=atomic ./...

# Test one
go test -v -run TestCalculateCost ./pkg/obs/...

# Lint
golangci-lint run

# Security
gosec ./...
govulncheck ./...
```

---

## EXTERNAL SERVICES

| Service | Purpose | Env var |
|---------|---------|---------|
| PostgreSQL/pgvector | All state: FSM, jobs, vectors, messages | `DB_HOST` `DB_PORT` etc. (via `pkg/db.Config`) |
| Redis | Ephemeral: rate limiting, session locks, embedding cache | (not yet wired) |
| OTEL collector | Distributed tracing | `OTEL_EXPORTER_OTLP_ENDPOINT` |
| Prometheus | Metrics scrape at `/metrics` | (built-in) |
| Ollama / OpenAI / Anthropic | LLM inference | (Phase 2+, not yet wired) |

---

## CI/CD

`.github/workflows/ci.yml` gates on `main` push + PRs:
- **build-and-test**: `go build ./...` + `go test -race -coverprofile`
- **lint**: `golangci-lint` v2.12.2
- **security**: `gosec` + `govulncheck`

---

## GOTCHAS

- **Tenant isolation is a hard invariant**: every DB query must include `tenant_id` via
  `tenant.MustGetTenantID(ctx)`. Missing it is an architectural violation, not just a bug.
- **Transaction boundaries (NFR-7)**: never hold a `pgxpool` transaction open during
  external API calls. River pattern: short txn → claim job → external work → short txn → complete.
- **Write Tool idempotency (NFR-4)**: any state-mutating external call needs an
  idempotency key + `AUDIT_LOG` record. Not yet implemented (Phase 3), but must be added
  before any Write Tool goes to production.
- **Phases 2–4 packages don't exist yet** — don't import or reference `pkg/runtime/`,
  `pkg/aicore/`, etc. until those phases start.
- **OTEL secure by default**: `OTEL_EXPORTER_OTLP_INSECURE=true` required for local collectors.
  Production always uses TLS.
- **3 source files need `gofmt`**: `cmd/server/main.go`, `pkg/db/db.go`, `pkg/obs/cost_test.go`.
- **`.env` is tracked by git** — it contains test credentials. Intentional for this personal
  project in early stage; do not add production secrets.
- Requirement IDs in code comments (`// NFR-N`, `// FR-N`) trace to `docs/requirements.md`.

---

## CONTRIBUTORS

```
19  Valentyn Solomko
```

---

Ready. Ask me about any layer.
