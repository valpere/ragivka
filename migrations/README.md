# Database Migrations

SQL files applied via [goose](https://github.com/pressly/goose) (`pkg/runtime.RunMigrations`,
called on every `cmd/server` and `cmd/worker` startup — idempotent, safe to re-run).
Files are embedded into the binary via `migrations.FS` (`migrations.go`).

## File Naming

```
NNN_<description>.sql   — NNN is a 3-digit sequence number
```

## Migrations

| File | Phase | Description |
|------|-------|-------------|
| `001_foundation.sql` | 1a | TENANT, USER — the absolute security boundary |
| `002_sessions.sql` | 1b | SESSION, MESSAGE — FSM + conversation history |
| `003_prompt_registry.sql` | 1c | PROMPT_VERSION — versioned system prompts |
| `004_artifacts.sql` | 2a | ARTIFACT — S3-backed document and generated artifact records |
| `005_knowledge.sql` | 2b | DOCUMENT, CHUNK (pgvector + tsvector) — RAG knowledge layer |
| `006_seed_mvp.sql` | 4 | Fixture data (1 tenant, 1 user, 3 FAQ documents) for the MVP integration test and smoke test |

## River Queue

River manages its own tables via `rivermigrate.Migrator.Up(ctx, nil)`.
Call this on every worker startup — it is idempotent.
River tables live in the same database alongside application tables.
Session → River link: River job `args` JSONB contains `session_id` and `tenant_id`.

## Local Dev

```bash
docker compose -f docker/docker-compose.yml up -d
go run ./cmd/server/   # applies all migrations on startup
```
