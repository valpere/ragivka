# ragivka

Multi-tenant RAG + workflow orchestration framework. See `CLAUDE.md` for
architecture, current implementation state, and development commands.

## Running the MVP

The MVP validates the framework end-to-end via an L1 Customer Support
Assistant (Issue #23, Case Study 3): a Telegram message triggers hybrid
RAG retrieval, a structured-output LLM call, an HITL confidence gate, and
a reply with traceable citations.

### 1. Start infrastructure

```bash
docker compose -f docker/docker-compose.yml up -d
```

This starts PostgreSQL (with pgvector), Redis, and MinIO. There is no local
Ollama container — by default the server talks to
[Ollama Cloud](https://ollama.com) over HTTPS (see step 2).

### 2. Configure environment

```bash
export OLLAMA_API_KEY=...            # required — Ollama Cloud API key
export TELEGRAM_BOT_TOKEN=...        # from @BotFather
export TELEGRAM_WEBHOOK_SECRET=...   # any random string; must match setWebhook's secret_token
export JWT_SECRET=...                # required for the web widget endpoints; any random string for local dev
```

`OLLAMA_MODEL` (default `qwen3.5:cloud`) and `OLLAMA_EMBED_MODEL` (default
`bge-m3:latest`) can be overridden to point at a local Ollama instance
instead — set `OLLAMA_API_URL=http://localhost:11434/api/chat` and
`OLLAMA_EMBED_URL=http://localhost:11434/api/embed` and `ollama pull` the
two models first.

### 3. Run the server

```bash
go run ./cmd/server/
```

This applies all migrations on startup, including `migrations/006_seed_mvp.sql`
— a fixture tenant, user, and 3 sample FAQ documents (return policy, shipping,
support hours) used by the smoke test and the `TestL1CustomerSupportFlow`
integration test (`integration/l1_customer_support_test.go`).

Point your bot's webhook at `POST /telegram/webhook/{tenantID}` (tenant ID
`00000000-0000-0000-0000-000000000001` for the seeded fixture) using
`TELEGRAM_WEBHOOK_SECRET` as the `secret_token` in Telegram's `setWebhook` call.

### 4. Verify

```bash
./scripts/smoke_test.sh
```

Checks `/health`, posts a synthetic Telegram update for the seeded tenant, and
(if `psql` is available) confirms a non-empty assistant reply with citations
was persisted.

`HITL_CONFIDENCE_THRESHOLD` (default `0.7`) controls when a low-confidence
answer escalates the session to `WaitingForHuman` instead of replying
automatically — see `pkg/tools.HITLGate` and `pkg/orchestrator.L1Handler`.