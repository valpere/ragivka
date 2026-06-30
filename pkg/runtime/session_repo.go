package runtime

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgSessionRepo struct{ pool *pgxpool.Pool }

// NewSessionRepository returns a PostgreSQL-backed SessionRepository.
func NewSessionRepository(pool *pgxpool.Pool) SessionRepository {
	return &pgSessionRepo{pool: pool}
}

func (r *pgSessionRepo) Create(ctx context.Context, s *Session) error {
	tenantID, err := tenantIDFromCtx(ctx)
	if err != nil {
		return err
	}
	s.TenantID = tenantID
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO session (id, tenant_id, user_id, state, version, orchestration_tier, channel, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		s.ID, tenantID, s.UserID, s.State, s.Version, s.Tier, s.Channel, s.ExpiresAt)
	return err
}

func (r *pgSessionRepo) GetByID(ctx context.Context, id uuid.UUID) (*Session, error) {
	tenantID, err := tenantIDFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	s := &Session{}
	err = r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, user_id, state, version, orchestration_tier, channel,
		       expires_at, created_at, updated_at
		FROM session
		WHERE id = $1 AND tenant_id = $2`,
		id, tenantID).Scan(
		&s.ID, &s.TenantID, &s.UserID, &s.State, &s.Version,
		&s.Tier, &s.Channel, &s.ExpiresAt, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}

func (r *pgSessionRepo) GetActiveByUserID(ctx context.Context, userID uuid.UUID) (*Session, error) {
	tenantID, err := tenantIDFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	s := &Session{}
	err = r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, user_id, state, version, orchestration_tier, channel,
		       expires_at, created_at, updated_at
		FROM session
		WHERE tenant_id = $1 AND user_id = $2 AND state IN ($3, $4)
		ORDER BY created_at DESC
		LIMIT 1`,
		tenantID, userID, string(StateActive), string(StateWaitingForHuman)).Scan(
		&s.ID, &s.TenantID, &s.UserID, &s.State, &s.Version,
		&s.Tier, &s.Channel, &s.ExpiresAt, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}

// Transition atomically advances the session state with optimistic locking (FR-6).
// The UPDATE matches only if version equals the caller's version; if another writer
// already incremented it, ErrOptimisticLock is returned.
func (r *pgSessionRepo) Transition(ctx context.Context, id uuid.UUID, from, to State, version int) (int, error) {
	if !Allowed(from, to) {
		return 0, ErrInvalidTransition{From: from, To: to}
	}
	tenantID, err := tenantIDFromCtx(ctx)
	if err != nil {
		return 0, err
	}
	var newVersion int
	err = r.pool.QueryRow(ctx, `
		UPDATE session
		SET state = $1, version = version + 1, updated_at = NOW()
		WHERE id = $2 AND tenant_id = $3 AND version = $4
		RETURNING version`,
		to, id, tenantID, version).Scan(&newVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrOptimisticLock
	}
	return newVersion, err
}

// ListExpired returns all sessions past their expiry that are still in a non-terminal state.
// This is intentionally a cross-tenant query — the expiry sweep runs for all tenants.
// Callers must inject each session's own TenantID into context before calling Transition.
func (r *pgSessionRepo) ListExpired(ctx context.Context) ([]*Session, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, user_id, state, version, orchestration_tier, channel,
		       expires_at, created_at, updated_at
		FROM session
		WHERE expires_at < NOW() AND state IN ($1, $2)`,
		string(StateActive), string(StateWaitingForHuman))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []*Session
	for rows.Next() {
		s := &Session{}
		if err := rows.Scan(&s.ID, &s.TenantID, &s.UserID, &s.State, &s.Version,
			&s.Tier, &s.Channel, &s.ExpiresAt, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}
