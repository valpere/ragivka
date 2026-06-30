package aicore

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/valpere/ragivka/pkg/tenant"
)

// ErrPromptNotFound is returned when no matching prompt_version row exists.
var ErrPromptNotFound = errors.New("prompt not found")

type pgPromptRegistry struct{ pool *pgxpool.Pool }

// NewPromptRegistry returns a PostgreSQL-backed PromptRegistry.
func NewPromptRegistry(pool *pgxpool.Pool) PromptRegistry {
	return &pgPromptRegistry{pool: pool}
}

// Load returns the content of the named prompt at the given version (FR-14).
func (r *pgPromptRegistry) Load(ctx context.Context, name string, version int) (string, error) {
	tenantID, err := tenantUUIDFromCtx(ctx)
	if err != nil {
		return "", err
	}
	var content string
	err = r.pool.QueryRow(ctx, `
		SELECT content
		FROM prompt_version
		WHERE tenant_id = $1 AND name = $2 AND version = $3`,
		tenantID, name, version).Scan(&content)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrPromptNotFound
	}
	return content, err
}

// LoadLatest returns the highest-version content for the named prompt (FR-14).
func (r *pgPromptRegistry) LoadLatest(ctx context.Context, name string) (string, error) {
	tenantID, err := tenantUUIDFromCtx(ctx)
	if err != nil {
		return "", err
	}
	var content string
	err = r.pool.QueryRow(ctx, `
		SELECT content
		FROM prompt_version
		WHERE tenant_id = $1 AND name = $2
		ORDER BY version DESC
		LIMIT 1`,
		tenantID, name).Scan(&content)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrPromptNotFound
	}
	return content, err
}

// tenantUUIDFromCtx extracts and parses the tenant ID from ctx.
func tenantUUIDFromCtx(ctx context.Context) (uuid.UUID, error) {
	raw := tenant.MustGetTenantID(ctx)
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("tenant ID in context is not a valid UUID: %w", err)
	}
	return id, nil
}
