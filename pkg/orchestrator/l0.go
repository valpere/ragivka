package orchestrator

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/valpere/ragivka/pkg/aicore"
	"github.com/valpere/ragivka/pkg/runtime"
	"github.com/valpere/ragivka/pkg/tenant"
)

// L0Handler executes FR-1: single-shot QA with no RAG (target < 3 s).
// User message → load history → LLM → save assistant message.
type L0Handler struct {
	deps
}

func NewL0Handler(router aicore.ModelRouter, sessions runtime.SessionRepository, messages runtime.MessageRepository) *L0Handler {
	return &L0Handler{deps: deps{router: router, sessions: sessions, messages: messages}}
}

func (h *L0Handler) Handle(ctx context.Context, session *runtime.Session, userMessage string) error {
	tctx := tenant.WithTenantID(ctx, session.TenantID.String())

	// Save user message.
	userMsg := &runtime.Message{
		ID:        uuid.New(),
		SessionID: session.ID,
		TenantID:  session.TenantID,
		Role:      "user",
		Content:   userMessage,
	}
	if err := h.messages.Create(tctx, userMsg); err != nil {
		return fmt.Errorf("l0: save user message: %w", err)
	}

	// Load conversation history for context window.
	history, err := h.messages.ListForSession(tctx, session.ID, 0)
	if err != nil {
		return fmt.Errorf("l0: load history: %w", err)
	}

	req := aicore.GenerateRequest{
		Messages: toAIMessages(history),
		TaskHint: aicore.TaskGeneration,
	}

	resp, err := h.router.Generate(tctx, req)
	if err != nil {
		return fmt.Errorf("l0: generate: %w", err)
	}

	assistantMsg := &runtime.Message{
		ID:        uuid.New(),
		SessionID: session.ID,
		TenantID:  session.TenantID,
		Role:      "assistant",
		Content:   resp.Content,
	}
	if err := h.messages.Create(tctx, assistantMsg); err != nil {
		return fmt.Errorf("l0: save assistant message: %w", err)
	}

	return nil
}

// toAIMessages converts runtime messages to aicore messages for the LLM call.
func toAIMessages(msgs []*runtime.Message) []aicore.Message {
	out := make([]aicore.Message, len(msgs))
	for i, m := range msgs {
		out[i] = aicore.Message{Role: m.Role, Content: m.Content}
	}
	return out
}
