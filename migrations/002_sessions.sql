-- Phase 1b: SESSION and MESSAGE
-- FR-5: FSM with four states. FR-6: optimistic locking via version. FR-7: expiry.
-- FR-23: context window retention enforced by message ordering + token_count.

-- ---------------------------------------------------------------------------
-- SESSION
-- ---------------------------------------------------------------------------
-- state:              Active | WaitingForHuman | Completed | Expired (FR-5)
-- version:            optimistic locking — increment on every state transition (FR-6)
-- orchestration_tier: L0 (single call) | L1 (sync tool) | L2 (async job) | L3 (graph)
-- expires_at:         set by session manager; FSM checks on every message receipt (FR-7)

CREATE TABLE IF NOT EXISTS session (
    id                  UUID        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    tenant_id           UUID        NOT NULL REFERENCES tenant(id) ON DELETE CASCADE,
    user_id             UUID        NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    state               TEXT        NOT NULL DEFAULT 'Active'
                            CHECK (state IN ('Active', 'WaitingForHuman', 'Completed', 'Expired')),
    version             INT         NOT NULL DEFAULT 0,
    orchestration_tier  TEXT        NOT NULL DEFAULT 'L1'
                            CHECK (orchestration_tier IN ('L0', 'L1', 'L2', 'L3')),
    channel             TEXT        NOT NULL, -- 'telegram' | 'web'
    expires_at          TIMESTAMPTZ NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Active session lookup: find all sessions for a user within a tenant
CREATE INDEX IF NOT EXISTS idx_session_tenant_user   ON session (tenant_id, user_id);
-- Expiry worker: find sessions past their expiry that are still Active/WaitingForHuman
CREATE INDEX IF NOT EXISTS idx_session_expiry        ON session (expires_at) WHERE state IN ('Active', 'WaitingForHuman');
-- State filter: used by orchestrator to load sessions by state
CREATE INDEX IF NOT EXISTS idx_session_tenant_state  ON session (tenant_id, state);

-- ---------------------------------------------------------------------------
-- MESSAGE
-- ---------------------------------------------------------------------------
-- role:          user | assistant | system
-- citation_refs: array of chunk UUIDs from the RAG layer (Phase 2); nullable for now
-- token_count:   prompt+completion tokens for the LLM call that produced this message
-- job_id:        River job ID for async replies (Phase 1b/1c); nullable for sync flows
-- tenant_id:     denormalized from session for direct tenant isolation checks (NFR-16)

CREATE TABLE IF NOT EXISTS message (
    id            UUID        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    session_id    UUID        NOT NULL REFERENCES session(id) ON DELETE CASCADE,
    tenant_id     UUID        NOT NULL REFERENCES tenant(id) ON DELETE CASCADE,
    role          TEXT        NOT NULL CHECK (role IN ('user', 'assistant', 'system')),
    content       TEXT        NOT NULL,
    citation_refs UUID[]      NULL,     -- populated in Phase 2 (RAG citations)
    token_count   INT         NULL,     -- null for user messages; set for assistant messages
    job_id        BIGINT      NULL,     -- River job ID; null for synchronous flows
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Conversation history retrieval (ordered by created_at within a session)
CREATE INDEX IF NOT EXISTS idx_message_session_id ON message (session_id, created_at);
-- Tenant isolation: scan all messages within a tenant (audit, cost attribution)
CREATE INDEX IF NOT EXISTS idx_message_tenant_id  ON message (tenant_id);
-- Async reply lookup: find the message associated with a specific River job
CREATE INDEX IF NOT EXISTS idx_message_job_id     ON message (job_id) WHERE job_id IS NOT NULL;
