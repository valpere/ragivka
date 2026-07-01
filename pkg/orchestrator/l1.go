package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/valpere/ragivka/pkg/aicore"
	"github.com/valpere/ragivka/pkg/knowledge/retrieval"
	"github.com/valpere/ragivka/pkg/runtime"
	"github.com/valpere/ragivka/pkg/tenant"
	"github.com/valpere/ragivka/pkg/tools"
)

const (
	l1TopK  = 5
	l1Alpha = 0.7 // blend: 70% vector + 30% keyword
)

// l1SystemInstruction requests structured JSON output (FR-15) so the
// HITLGate has a machine-readable confidence score to evaluate (FR-18).
const l1SystemInstruction = `You must respond with ONLY a JSON object matching this exact shape, no other text:
{"answer": "<your answer to the user's question, grounded in the provided context>", "confidence": <float 0.0-1.0, your confidence that the answer is correct and fully supported by the context>, "requires_human": <true if you are unsure, the context does not contain the answer, or the question needs human judgement>}`

// l1StructuredAnswer is the shape requested by l1SystemInstruction (FR-15).
type l1StructuredAnswer struct {
	Answer        string  `json:"answer"`
	Confidence    float64 `json:"confidence"`
	RequiresHuman bool    `json:"requires_human"`
}

// L1Handler executes FR-2: single RAG pass with hybrid retrieval (target < 10 s, sync).
// User message → save → embed query → retrieve → inject top-K → LLM (structured output) →
// HITL gate (FR-18) → save reply with citations, or escalate to WaitingForHuman.
type L1Handler struct {
	deps
}

func NewL1Handler(
	router aicore.ModelRouter,
	sessions runtime.SessionRepository,
	messages runtime.MessageRepository,
	retriever retrieval.Retriever,
	hitl *tools.HITLGate,
) *L1Handler {
	return &L1Handler{deps: deps{
		router:    router,
		sessions:  sessions,
		messages:  messages,
		retriever: retriever,
		hitl:      hitl,
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
		Messages:  messages,
		TaskHint:  aicore.TaskGeneration,
		ForceJSON: true,
	}

	resp, err := h.router.Generate(tctx, req)
	if err != nil {
		return fmt.Errorf("l1: generate: %w", err)
	}

	answer, escalate := parseStructuredAnswer(resp.Content)
	if !escalate {
		if err := h.hitl.Evaluate(answer.Confidence); err != nil {
			if errors.Is(err, tools.ErrHITLRequired) {
				escalate = true
			} else {
				return fmt.Errorf("l1: hitl evaluate: %w", err)
			}
		}
	}
	if !escalate && answer.RequiresHuman {
		escalate = true
	}

	if escalate {
		if _, err := h.sessions.Transition(tctx, session.ID, session.State, runtime.StateWaitingForHuman, session.Version); err != nil {
			return fmt.Errorf("l1: escalate to WaitingForHuman: %w", err)
		}
		return nil
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
		Content:      answer.Answer,
		CitationRefs: chunkIDs,
	}
	if err := h.messages.Create(tctx, assistantMsg); err != nil {
		return fmt.Errorf("l1: save assistant message: %w", err)
	}

	return nil
}

// parseStructuredAnswer parses the model's JSON response (FR-15). A response
// that fails to parse cannot be trusted, so it fails safe: escalate to a
// human rather than risk surfacing an unvalidated answer (FR-18).
func parseStructuredAnswer(content string) (l1StructuredAnswer, bool) {
	answer, err := aicore.ParseStructured[l1StructuredAnswer](content)
	if err != nil {
		return l1StructuredAnswer{}, true
	}
	return answer, false
}

// buildRAGMessages prepends a system message with retrieved context, followed
// by the structured-output instruction, to the conversation history.
func buildRAGMessages(history []*runtime.Message, chunks []retrieval.RankedChunk) []aicore.Message {
	var out []aicore.Message
	if len(chunks) > 0 {
		out = append(out, aicore.Message{
			Role:    "system",
			Content: buildContextBlock(chunks),
		})
	}
	out = append(out, aicore.Message{Role: "system", Content: l1SystemInstruction})
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
