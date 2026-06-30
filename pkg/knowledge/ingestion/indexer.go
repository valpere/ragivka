package ingestion

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/valpere/ragivka/pkg/knowledge"
	"github.com/valpere/ragivka/pkg/tenant"
)

// Indexer persists embedded chunks into PostgreSQL (FR-9).
type Indexer interface {
	Index(ctx context.Context, docID uuid.UUID, chunks []knowledge.Chunk) error
}

type pgIndexer struct{ pool *pgxpool.Pool }

// NewIndexer returns a PostgreSQL-backed Indexer.
func NewIndexer(pool *pgxpool.Pool) Indexer {
	return &pgIndexer{pool: pool}
}

// Index bulk-inserts chunks for docID using pgx SendBatch (FR-9, NFR-4).
// Uses ON CONFLICT DO NOTHING on (tenant_id, document_id, chunk_index) so that
// re-running an ingestion job after a partial failure is safe — existing chunks
// are skipped rather than duplicated.
// The calling context must carry a tenant ID (NFR-16).
func (idx *pgIndexer) Index(ctx context.Context, docID uuid.UUID, chunks []knowledge.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	tenantID, err := tenantUUIDFromCtx(ctx)
	if err != nil {
		return err
	}

	const q = `
INSERT INTO chunk (id, tenant_id, document_id, content, token_count, chunk_index, embedding)
VALUES ($1, $2, $3, $4, $5, $6, $7::vector)
ON CONFLICT (tenant_id, document_id, chunk_index) DO NOTHING`

	batch := &pgx.Batch{}
	for _, c := range chunks {
		batch.Queue(q, uuid.New(), tenantID, docID, c.Content, c.TokenCount, c.ChunkIndex, vectorLiteral(c.Embedding))
	}

	results := idx.pool.SendBatch(ctx, batch)
	defer func() { _ = results.Close() }()

	for range chunks {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("indexer: insert chunk: %w", err)
		}
	}
	return results.Close()
}

// vectorLiteral formats a float32 slice as the PostgreSQL vector literal expected by ::vector.
// Example: [0.1, 0.2, 0.3] → "[0.1,0.2,0.3]"
func vectorLiteral(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	var sb strings.Builder
	sb.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	sb.WriteByte(']')
	return sb.String()
}

// DocumentRepository implements knowledge.DocumentRepository using pgx.
type DocumentRepository struct{ pool *pgxpool.Pool }

// NewDocumentRepository returns a PostgreSQL-backed DocumentRepository.
func NewDocumentRepository(pool *pgxpool.Pool) knowledge.DocumentRepository {
	return &DocumentRepository{pool: pool}
}

func (r *DocumentRepository) Create(ctx context.Context, d *knowledge.Document) error {
	tenantID, err := tenantUUIDFromCtx(ctx)
	if err != nil {
		return err
	}
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	d.TenantID = tenantID
	_, err = r.pool.Exec(ctx, `
		INSERT INTO document (id, tenant_id, type, filename, s3_key, size_bytes, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		d.ID, tenantID, d.Type, d.Filename, d.S3Key, d.SizeBytes, string(d.Status))
	return err
}

func (r *DocumentRepository) GetByID(ctx context.Context, id uuid.UUID) (*knowledge.Document, error) {
	tenantID, err := tenantUUIDFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	var d knowledge.Document
	err = r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, type, filename, s3_key, size_bytes, status, COALESCE(error_msg,''), created_at, updated_at
		FROM document
		WHERE tenant_id = $1 AND id = $2`,
		tenantID, id).
		Scan(&d.ID, &d.TenantID, &d.Type, &d.Filename, &d.S3Key, &d.SizeBytes,
			&d.Status, &d.ErrorMsg, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, knowledge.ErrNotFound
	}
	return &d, err
}

func (r *DocumentRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status knowledge.DocumentStatus, errMsg string) error {
	tenantID, err := tenantUUIDFromCtx(ctx)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE document SET status = $3, error_msg = NULLIF($4,'')
		WHERE tenant_id = $1 AND id = $2`,
		tenantID, id, string(status), errMsg)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return knowledge.ErrNotFound
	}
	return nil
}

// tenantUUIDFromCtx is a local helper to avoid import cycle with pkg/knowledge.
func tenantUUIDFromCtx(ctx context.Context) (uuid.UUID, error) {
	raw, err := tenant.GetTenantID(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("tenant ID in context is not a valid UUID: %w", err)
	}
	return id, nil
}
