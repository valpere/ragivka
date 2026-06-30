-- +goose Up
-- Phase 1a: TENANT and USER
-- The absolute security boundary — every downstream table references tenant_id.
-- NFR-16: all queries must carry tenant_id; cross-tenant access is architectural violation.

-- Enable required extensions
CREATE EXTENSION IF NOT EXISTS "pgcrypto"; -- gen_random_uuid()

-- ---------------------------------------------------------------------------
-- TENANT
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS tenant (
    id          UUID        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    name        TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ---------------------------------------------------------------------------
-- USER
-- ---------------------------------------------------------------------------
-- channel_type: the channel this user interacts through.
-- channel_id:   provider-specific identifier (Telegram user_id, widget session token, etc.).
-- UNIQUE constraint prevents duplicate registrations per tenant+channel combination.

CREATE TABLE IF NOT EXISTS "user" (
    id           UUID        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    tenant_id    UUID        NOT NULL REFERENCES tenant(id) ON DELETE CASCADE,
    channel_type TEXT        NOT NULL, -- 'telegram' | 'web' (extensible — avoid enum for now)
    channel_id   TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT user_tenant_channel_unique UNIQUE (tenant_id, channel_type, channel_id),
    -- Composite unique enables FK from session(tenant_id, user_id) → here, enforcing cross-table tenant isolation.
    CONSTRAINT user_tenant_id_unique UNIQUE (tenant_id, id)
);

-- idx_user_tenant_id removed — user_tenant_channel_unique already creates an index
-- with tenant_id as prefix, covering WHERE tenant_id = $1 queries.
