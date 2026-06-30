package runtime

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgMessageRepo struct{ pool *pgxpool.Pool }

// NewMessageRepository returns a PostgreSQL-backed MessageRepository.
func NewMessageRepository(pool *pgxpool.Pool) MessageRepository {
	return &pgMessageRepo{pool: pool}
}

func (r *pgMessageRepo) Create(ctx context.Context, m *Message) error {
	tenantID, err := tenantIDFromCtx(ctx)
	if err != nil {
		return err
	}
	m.TenantID = tenantID
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO message (id, session_id, tenant_id, role, content, citation_refs, token_count, job_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		m.ID, m.SessionID, tenantID, m.Role, m.Content, m.CitationRefs, m.TokenCount, m.JobID)
	return err
}

// ListForSession returns messages in chronological order, capped by maxTokens (FR-23).
// Loads newest-first, accumulates token budget, then reverses for caller convenience.
// Messages with nil TokenCount do not consume budget.
func (r *pgMessageRepo) ListForSession(ctx context.Context, sessionID uuid.UUID, maxTokens int) ([]*Message, error) {
	tenantID, err := tenantIDFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, session_id, tenant_id, role, content, citation_refs, token_count, job_id, created_at
		FROM message
		WHERE session_id = $1 AND tenant_id = $2
		ORDER BY created_at DESC`,
		sessionID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []*Message
	unlimited := maxTokens <= 0
	budget := maxTokens
	for rows.Next() {
		m := &Message{}
		if err := rows.Scan(&m.ID, &m.SessionID, &m.TenantID, &m.Role, &m.Content,
			&m.CitationRefs, &m.TokenCount, &m.JobID, &m.CreatedAt); err != nil {
			return nil, err
		}
		if !unlimited && m.TokenCount != nil {
			budget -= *m.TokenCount
			if budget < 0 {
				break
			}
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Reverse to chronological order.
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}

func (r *pgMessageRepo) GetByJobID(ctx context.Context, jobID int64) (*Message, error) {
	tenantID, err := tenantIDFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	m := &Message{}
	err = r.pool.QueryRow(ctx, `
		SELECT id, session_id, tenant_id, role, content, citation_refs, token_count, job_id, created_at
		FROM message
		WHERE job_id = $1 AND tenant_id = $2`,
		jobID, tenantID).Scan(
		&m.ID, &m.SessionID, &m.TenantID, &m.Role, &m.Content,
		&m.CitationRefs, &m.TokenCount, &m.JobID, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}
