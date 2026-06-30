package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/valpere/ragivka/pkg/tenant"
)

type pgArtifactRepository struct{ pool *pgxpool.Pool }

// NewArtifactRepository returns a PostgreSQL-backed ArtifactRepository.
func NewArtifactRepository(pool *pgxpool.Pool) ArtifactRepository {
	return &pgArtifactRepository{pool: pool}
}

// Create inserts a new artifact record for the tenant in context (NFR-16).
func (r *pgArtifactRepository) Create(ctx context.Context, a *Artifact) error {
	tenantID, err := tenantUUIDFromCtx(ctx)
	if err != nil {
		return err
	}
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	a.TenantID = tenantID

	_, err = r.pool.Exec(ctx, `
		INSERT INTO artifact (id, tenant_id, session_id, type, s3_key, size_bytes)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		a.ID, tenantID, a.SessionID, a.Type, a.S3Key, a.SizeBytes)
	return err
}

// GetByID fetches an artifact by primary key, scoped to the tenant in context (NFR-16).
func (r *pgArtifactRepository) GetByID(ctx context.Context, id uuid.UUID) (*Artifact, error) {
	tenantID, err := tenantUUIDFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	var a Artifact
	err = r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, session_id, type, s3_key, size_bytes, created_at
		FROM artifact
		WHERE tenant_id = $1 AND id = $2`,
		tenantID, id).
		Scan(&a.ID, &a.TenantID, &a.SessionID, &a.Type, &a.S3Key, &a.SizeBytes, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ListForSession returns all artifacts for a session, scoped to the tenant in context (NFR-16).
func (r *pgArtifactRepository) ListForSession(ctx context.Context, sessionID uuid.UUID) ([]*Artifact, error) {
	tenantID, err := tenantUUIDFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, session_id, type, s3_key, size_bytes, created_at
		FROM artifact
		WHERE tenant_id = $1 AND session_id = $2
		ORDER BY created_at ASC`,
		tenantID, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*Artifact
	for rows.Next() {
		var a Artifact
		if err := rows.Scan(&a.ID, &a.TenantID, &a.SessionID, &a.Type, &a.S3Key, &a.SizeBytes, &a.CreatedAt); err != nil {
			return nil, err
		}
		results = append(results, &a)
	}
	return results, rows.Err()
}

// Delete removes an artifact record by ID, scoped to the tenant in context (NFR-16).
func (r *pgArtifactRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tenantID, err := tenantUUIDFromCtx(ctx)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM artifact WHERE tenant_id = $1 AND id = $2`,
		tenantID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// tenantUUIDFromCtx extracts and parses the tenant ID, returning an error (not panicking)
// on missing tenant so callers receive a proper error boundary (NFR-16).
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
