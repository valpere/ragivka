-- +goose Up
-- Phase 2b: DOCUMENT and CHUNK tables for the RAG knowledge layer (FR-8, FR-9, FR-10).
-- Documents are tenant-scoped; chunks carry composite FK to enforce tenant isolation (NFR-16).

-- ---------------------------------------------------------------------------
-- DOCUMENT — raw document registry; S3 holds the actual bytes (FR-20)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS document (
    id          UUID        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    tenant_id   UUID        NOT NULL REFERENCES tenant(id) ON DELETE CASCADE,
    type        TEXT        NOT NULL, -- 'txt' | 'html' | 'pdf' | 'docx'
    filename    TEXT        NOT NULL,
    s3_key      TEXT        NOT NULL,
    size_bytes  BIGINT      NOT NULL DEFAULT 0,
    -- status lifecycle: pending → processing → ready | failed
    status      TEXT        NOT NULL DEFAULT 'pending',
    error_msg   TEXT,                -- non-null only when status = 'failed'
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT document_tenant_id_unique UNIQUE (tenant_id, id)
);

CREATE INDEX IF NOT EXISTS idx_document_tenant_status ON document (tenant_id, status);

-- Reuse set_updated_at trigger function from migration 002.
CREATE TRIGGER document_updated_at
    BEFORE UPDATE ON document
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- CHUNK — chunked and embedded content, ready for hybrid retrieval (FR-9, FR-10)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS chunk (
    id          UUID        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    tenant_id   UUID        NOT NULL REFERENCES tenant(id) ON DELETE CASCADE,
    document_id UUID        NOT NULL,
    content     TEXT        NOT NULL,
    token_count INT         NOT NULL DEFAULT 0,
    chunk_index INT         NOT NULL DEFAULT 0,
    -- bge-m3:latest produces 1024-dimensional embeddings (NFR-8 / FR-9)
    embedding   VECTOR(1024),
    -- Pre-computed tsvector for efficient keyword search (FR-10 hybrid retrieval)
    tsv         TSVECTOR GENERATED ALWAYS AS (to_tsvector('english', content)) STORED,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chunk_tenant_doc_fk
        FOREIGN KEY (tenant_id, document_id)
        REFERENCES document(tenant_id, id)
        ON DELETE CASCADE
);

-- IVFFlat index for approximate nearest-neighbour search (FR-10).
-- lists=100 is appropriate for up to ~1M vectors per index.
CREATE INDEX IF NOT EXISTS idx_chunk_embedding
    ON chunk USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);

CREATE INDEX IF NOT EXISTS idx_chunk_tsv ON chunk USING gin (tsv);
CREATE INDEX IF NOT EXISTS idx_chunk_document ON chunk (tenant_id, document_id, chunk_index);
