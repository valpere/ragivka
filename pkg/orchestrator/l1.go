package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/valpere/ragivka/pkg/aicore"
	"github.com/valpere/ragivka/pkg/knowledge/retrieval"
	"github.com/valpere/ragivka/pkg/runtime"
	"github.com/valpere/ragivka/pkg/tenant"
)

const (
	l1TopK  = 5
	l1Alpha = 0.7 // blend: 70% vector + 30% keyword
)

// L1Handler executes FR-2: single RAG pass with hybrid retrieval (target < 10 s, sync).
// User message → save → embed query → retrieve → inject top-K → LLM → save with citations.
type L1Handler struct {
	deps
}

func NewL1Handler(
	router aicore.ModelRouter,
	sessions runtime.SessionRepository,
	messages runtime.MessageRepository,
	retriever retrieval.Retriever,
) *L1Handler {
	return &L1Handler{deps: deps{
		router:    router,
		sessions:  sessions,
		messages:  messages,
		retriever: retriever,
	}}
}

func (h *L1Handler) Handle(ctx context.Context, session *runtime.Session, userMessage string) error {
	tctx := tenant.WithTenantID(ctx, session.TenantID.String())

	userMsg := &runtime.Message{
		ID:        uuid.New(),
		SessionID: session.ID,
		TenantID:  session.TenantID,
		Role:      "user",
		Content:   userMessage,
	}
	if err := h.messages.Create(tctx, userMsg); err != nil {
		return fmt.Errorf("l1: save user message: %w", err)
	}

	// Retrieve top-K chunks.
	chunks, err := h.retriever.Retrieve(tctx, userMessage, l1TopK, l1Alpha)
	if err != nil {
		return fmt.Errorf("l1: retrieve: %w", err)
	}

	// Load history and inject retrieved context as a system message.
	history, err := h.messages.ListForSession(tctx, session.ID, 0)
	if err != nil {
		return fmt.Errorf("l1: load history: %w", err)
	}

	messages := buildRAGMessages(history, chunks)
	req := aicore.GenerateRequest{
		Messages: messages,
		TaskHint: aicore.TaskGeneration,
	}

	resp, err := h.router.Generate(tctx, req)
	if err != nil {
		return fmt.Errorf("l1: generate: %w", err)
	}

	chunkIDs := make([]uuid.UUID, len(chunks))
	for i, c := range chunks {
		chunkIDs[i] = c.ChunkID
	}

	assistantMsg := &runtime.Message{
		ID:           uuid.New(),
		SessionID:    session.ID,
		TenantID:     session.TenantID,
		Role:         "assistant",
		Content:      resp.Content,
		CitationRefs: chunkIDs,
	}
	if err := h.messages.Create(tctx, assistantMsg); err != nil {
		return fmt.Errorf("l1: save assistant message: %w", err)
	}

	return nil
}

// buildRAGMessages prepends a system message with retrieved context to the conversation history.
func buildRAGMessages(history []*runtime.Message, chunks []retrieval.RankedChunk) []aicore.Message {
	var out []aicore.Message
	if len(chunks) > 0 {
		out = append(out, aicore.Message{
			Role:    "system",
			Content: buildContextBlock(chunks),
		})
	}
	for _, m := range history {
		out = append(out, aicore.Message{Role: m.Role, Content: m.Content})
	}
	return out
}

// buildContextBlock formats retrieved chunks into a numbered reference block.
func buildContextBlock(chunks []retrieval.RankedChunk) string {
	var sb strings.Builder
	sb.WriteString("Use the following retrieved context to answer the question:\n\n")
	for i, c := range chunks {
		fmt.Fprintf(&sb, "[%d] %s\n\n", i+1, c.Content)
	}
	return sb.String()
}
