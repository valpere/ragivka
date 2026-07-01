-- +goose Up
-- Phase 4 MVP fixture data (Issue #23): 1 tenant, 1 user, 3 sample FAQ documents
-- with pre-chunked content, for the L1 Customer Support Assistant integration
-- test and scripts/smoke_test.sh. Fixed UUIDs make this migration idempotent
-- (ON CONFLICT DO NOTHING) and safe to re-run against a database that already
-- has this fixture applied.
--
-- Chunk embeddings are intentionally NULL: generating real bge-m3 vectors
-- requires a running Ollama instance, which a SQL migration cannot call.
-- pkg/knowledge/retrieval's hybrid query COALESCEs a NULL embedding's vector
-- score to 0.0, so retrieval still works via keyword (tsvector) matching —
-- sufficient for exercising the L1 flow end-to-end.

INSERT INTO tenant (id, name)
VALUES ('00000000-0000-0000-0000-000000000001', 'MVP Demo Tenant')
ON CONFLICT (id) DO NOTHING;

INSERT INTO "user" (id, tenant_id, channel_type, channel_id)
VALUES (
    '00000000-0000-0000-0000-000000000002',
    '00000000-0000-0000-0000-000000000001',
    'telegram',
    'smoke-test-user'
)
ON CONFLICT (tenant_id, channel_type, channel_id) DO NOTHING;

INSERT INTO document (id, tenant_id, type, filename, s3_key, size_bytes, status)
VALUES
    ('00000000-0000-0000-0000-000000000101', '00000000-0000-0000-0000-000000000001',
     'txt', 'faq-returns.txt', 'seed/faq-returns.txt', 0, 'ready'),
    ('00000000-0000-0000-0000-000000000102', '00000000-0000-0000-0000-000000000001',
     'txt', 'faq-shipping.txt', 'seed/faq-shipping.txt', 0, 'ready'),
    ('00000000-0000-0000-0000-000000000103', '00000000-0000-0000-0000-000000000001',
     'txt', 'faq-support-hours.txt', 'seed/faq-support-hours.txt', 0, 'ready')
ON CONFLICT (tenant_id, id) DO NOTHING;

INSERT INTO chunk (tenant_id, document_id, content, token_count, chunk_index)
VALUES
    ('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000101',
     'You can return any unopened item within 30 days of delivery for a full refund. '
     || 'Opened items are eligible for a 50% refund within 14 days. To start a return, '
     || 'contact support with your order number.',
     0, 0),
    ('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000102',
     'Standard shipping takes 3-5 business days and is free on orders over $50. '
     || 'Express shipping takes 1-2 business days for a flat fee of $12. We currently '
     || 'ship to the United States, Canada, and the European Union.',
     0, 0),
    ('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000103',
     'Customer support is available Monday through Friday, 9 AM to 6 PM Eastern Time. '
     || 'Outside these hours, messages are queued and answered on the next business day.',
     0, 0)
ON CONFLICT (tenant_id, document_id, chunk_index) DO NOTHING;
