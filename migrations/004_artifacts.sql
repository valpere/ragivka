-- +goose Up
-- Phase 2a: ARTIFACT table for S3-backed document and generated artifact records (FR-20).
-- Every artifact is scoped by tenant_id; composite FK enforces cross-table tenant isolation.

CREATE TABLE IF NOT EXISTS artifact (
    id          UUID        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    tenant_id   UUID        NOT NULL REFERENCES tenant(id) ON DELETE CASCADE,
    session_id  UUID        NOT NULL,
    -- type: 'raw_document' | 'pdf' | 'excel' | 'summary' (open string — avoid enum for extensibility)
    type        TEXT        NOT NULL,
    s3_key      TEXT        NOT NULL,
    size_bytes  BIGINT      NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Composite FK ensures artifact.session_id belongs to the same tenant (NFR-16).
    CONSTRAINT artifact_tenant_session_fk
        FOREIGN KEY (tenant_id, session_id)
        REFERENCES session(tenant_id, id)
        ON DELETE CASCADE,

    -- Composite unique enables FK from child tables to artifact without cross-tenant leakage.
    CONSTRAINT artifact_tenant_id_unique UNIQUE (tenant_id, id)
);

CREATE INDEX IF NOT EXISTS idx_artifact_session_id ON artifact (tenant_id, session_id);
