package ingestion

import (
	"context"
	"errors"
	"fmt"

	pgvector "github.com/pgvector/pgvector-go"
	pgxvector "github.com/pgvector/pgvector-go/pgx"
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

// Index bulk-inserts chunks for docID using pgx CopyFrom (FR-9).
// The calling context must carry a tenant ID (NFR-16).
func (idx *pgIndexer) Index(ctx context.Context, docID uuid.UUID, chunks []knowledge.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	raw, err := tenant.GetTenantID(ctx)
	if err != nil {
		return err
	}
	tenantID, err := uuid.Parse(raw)
	if err != nil {
		return fmt.Errorf("tenant ID not a valid UUID: %w", err)
	}

	conn, err := idx.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("indexer: acquire conn: %w", err)
	}
	defer conn.Release()

	// Register pgvector types so pgx can encode/decode VECTOR columns.
	if err := pgxvector.RegisterTypes(ctx, conn.Conn()); err != nil {
		return fmt.Errorf("indexer: register pgvector types: %w", err)
	}

	rows := make([][]any, len(chunks))
	for i, c := range chunks {
		vec := pgvector.NewVector(c.Embedding)
		rows[i] = []any{uuid.New(), tenantID, docID, c.Content, c.TokenCount, c.ChunkIndex, vec}
	}

	_, err = conn.Conn().CopyFrom(
		ctx,
		pgx.Identifier{"chunk"},
		[]string{"id", "tenant_id", "document_id", "content", "token_count", "chunk_index", "embedding"},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return fmt.Errorf("indexer: copy chunks: %w", err)
	}
	return nil
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
