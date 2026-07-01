package runtime

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserRepository maps a channel-specific identity (Telegram user ID, web
// widget token, etc.) to a stable internal user UUID. All methods extract
// tenant_id from ctx via tenant.MustGetTenantID (NFR-16).
type UserRepository interface {
	// ResolveOrCreate returns the internal user ID for (channelType, channelID),
	// creating a USER row on first contact. Idempotent: the underlying upsert
	// is safe under concurrent calls for the same identity (NFR-4).
	ResolveOrCreate(ctx context.Context, channelType, channelID string) (uuid.UUID, error)
}

type pgUserRepo struct{ pool *pgxpool.Pool }

// NewUserRepository returns a PostgreSQL-backed UserRepository.
func NewUserRepository(pool *pgxpool.Pool) UserRepository {
	return &pgUserRepo{pool: pool}
}

func (r *pgUserRepo) ResolveOrCreate(ctx context.Context, channelType, channelID string) (uuid.UUID, error) {
	tenantID, err := tenantIDFromCtx(ctx)
	if err != nil {
		return uuid.UUID{}, err
	}

	var userID uuid.UUID
	err = r.pool.QueryRow(ctx, `
		INSERT INTO "user" (tenant_id, channel_type, channel_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (tenant_id, channel_type, channel_id) DO UPDATE
			SET channel_type = EXCLUDED.channel_type
		RETURNING id`,
		tenantID, channelType, channelID).Scan(&userID)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("user repo: resolve or create: %w", err)
	}
	return userID, nil
}
