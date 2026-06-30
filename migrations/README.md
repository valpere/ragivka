# Database Migrations

Pure SQL files — tooling is not yet decided (candidates: goose, golang-migrate, atlas).
Each file is idempotent and can be applied with `psql -f <file>` for local dev.

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
| `004_knowledge.sql` | 2 | DOCUMENT, CHUNK (pgvector + tsvector) — TBD |
| `005_tools.sql` | 3 | AUDIT_LOG, ARTIFACT — TBD |

## River Queue

River manages its own tables via `rivermigrate.Migrator.Up(ctx, nil)`.
Call this on every worker startup — it is idempotent.
River tables live in the same database alongside application tables.
Session → River link: River job `args` JSONB contains `session_id` and `tenant_id`.

## Local Dev

```bash
docker compose -f docker/docker-compose.yml up -d
psql postgresql://ragivka:ragivka_password@localhost:5432/ragivka_db -f migrations/001_foundation.sql
psql postgresql://ragivka:ragivka_password@localhost:5432/ragivka_db -f migrations/002_sessions.sql
psql postgresql://ragivka:ragivka_password@localhost:5432/ragivka_db -f migrations/003_prompt_registry.sql
```
