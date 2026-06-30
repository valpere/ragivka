-- +goose Up
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

    -- A specific (tenant, name, version) combination must be unique.
    -- PostgreSQL creates a B-tree index on (tenant_id, name, version ASC) for this constraint;
    -- PG can scan it in reverse for ORDER BY version DESC, so no separate DESC index needed.
    CONSTRAINT prompt_version_unique UNIQUE (tenant_id, name, version)
);
