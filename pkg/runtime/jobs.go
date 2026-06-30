package runtime

import (
	"context"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
)

// GenerateResponseArgs is the River job payload for async LLM generation (Phase 1c).
// NFR-4/NFR-15: IdempotencyKey must be checked before executing to prevent double-generation.
type GenerateResponseArgs struct {
	TenantID       uuid.UUID `json:"tenant_id"`
	SessionID      uuid.UUID `json:"session_id"`
	MessageID      uuid.UUID `json:"message_id"`
	IdempotencyKey string    `json:"idempotency_key"`
}

func (GenerateResponseArgs) Kind() string { return "generate_response" }

// ExpireSessionsArgs is the River job payload for the periodic session expiry sweep (FR-7).
type ExpireSessionsArgs struct{}

func (ExpireSessionsArgs) Kind() string { return "expire_sessions" }

// GenerateResponseWorker is a stub — real implementation in Phase 1c (pkg/aicore).
type GenerateResponseWorker struct {
	river.WorkerDefaults[GenerateResponseArgs]
}

func (w *GenerateResponseWorker) Work(_ context.Context, _ *river.Job[GenerateResponseArgs]) error {
	return nil
}

// ExpireSessionsWorker marks timed-out sessions as Expired (FR-7).
type ExpireSessionsWorker struct {
	river.WorkerDefaults[ExpireSessionsArgs]
	sessions SessionRepository
}

func NewExpireSessionsWorker(sessions SessionRepository) *ExpireSessionsWorker {
	return &ExpireSessionsWorker{sessions: sessions}
}

func (w *ExpireSessionsWorker) Work(ctx context.Context, _ *river.Job[ExpireSessionsArgs]) error {
	expired, err := w.sessions.ListExpired(ctx)
	if err != nil {
		return err
	}
	for _, s := range expired {
		if _, err := w.sessions.Transition(ctx, s.ID, s.State, StateExpired, s.Version); err != nil {
			return err
		}
	}
	return nil
}
