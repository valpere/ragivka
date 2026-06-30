-- Phase 1c: PROMPT_VERSION
-- FR-14: version-controlled storage for system prompts.
-- Prompts are loaded by (tenant_id, name, version) — prevents drift between deployments.
-- Tenant-scoped: each tenant can have its own prompt variants (custom personas, languages).

CREATE TABLE IF NOT EXISTS prompt_version (
    id          UUID        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    tenant_id   UUID        NOT NULL REFERENCES tenant(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,   -- logical prompt identifier, e.g. 'support_assistant'
    version     INT         NOT NULL DEFAULT 1,
    content     TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- A specific (tenant, name, version) combination must be unique
    CONSTRAINT prompt_version_unique UNIQUE (tenant_id, name, version)
);

-- Fast lookup by name, returning latest version first
CREATE INDEX IF NOT EXISTS idx_prompt_version_lookup ON prompt_version (tenant_id, name, version DESC);
