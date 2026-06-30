package orchestrator

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/valpere/ragivka/pkg/runtime"
	"github.com/valpere/ragivka/pkg/tenant"
)

// L2Handler executes FR-3: async multi-step pipeline.
// Saves user message then enqueues a GenerateResponseArgs River job and returns immediately.
// Response is delivered via channel adapter callback (no sync latency constraint, NFR-1).
type L2Handler struct {
	deps
}

func NewL2Handler(
	sessions runtime.SessionRepository,
	messages runtime.MessageRepository,
	enqueuer JobEnqueuer,
) *L2Handler {
	return &L2Handler{deps: deps{
		sessions: sessions,
		messages: messages,
		enqueuer: enqueuer,
	}}
}

func (h *L2Handler) Handle(ctx context.Context, session *runtime.Session, userMessage string) error {
	tctx := tenant.WithTenantID(ctx, session.TenantID.String())

	userMsg := &runtime.Message{
		ID:        uuid.New(),
		SessionID: session.ID,
		TenantID:  session.TenantID,
		Role:      "user",
		Content:   userMessage,
	}
	if err := h.messages.Create(tctx, userMsg); err != nil {
		return fmt.Errorf("l2: save user message: %w", err)
	}

	// Fire-and-forget: enqueue async generation job (FR-3).
	// IdempotencyKey = userMsg.ID so a River retry won't double-generate (NFR-4).
	if err := h.enqueuer.EnqueueGenerateResponse(tctx, runtime.GenerateResponseArgs{
		TenantID:       session.TenantID,
		SessionID:      session.ID,
		MessageID:      userMsg.ID,
		IdempotencyKey: userMsg.ID.String(),
	}); err != nil {
		return fmt.Errorf("l2: enqueue: %w", err)
	}

	return nil
}
