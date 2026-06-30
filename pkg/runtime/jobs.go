package runtime

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/valpere/ragivka/pkg/tenant"
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
// ListExpired is a cross-tenant query (no tenant in context); each session's own
// TenantID is injected into context before calling Transition so NFR-16 is preserved.
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
		// Inject this session's tenant so Transition's tenantIDFromCtx succeeds (NFR-16).
		tctx := tenant.WithTenantID(ctx, s.TenantID.String())
		_, err := w.sessions.Transition(tctx, s.ID, s.State, StateExpired, s.Version)
		if errors.Is(err, ErrOptimisticLock) {
			// Another process already transitioned this session — skip it.
			continue
		}
		if err != nil {
			return err
		}
	}
	return nil
}
