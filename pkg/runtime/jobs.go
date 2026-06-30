package runtime

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/valpere/ragivka/pkg/aicore"
	"github.com/valpere/ragivka/pkg/obs"
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

// GenerateResponseWorker calls the LLM router to produce an assistant reply (FR-13, Phase 1c).
// NFR-4/NFR-15: checks GetByJobID before generating to prevent double-execution.
// NFR-7: LLM call is outside any DB transaction.
type GenerateResponseWorker struct {
	river.WorkerDefaults[GenerateResponseArgs]
	messages MessageRepository
	sessions SessionRepository
	router   aicore.ModelRouter
	registry aicore.PromptRegistry
}

// NewGenerateResponseWorker constructs a GenerateResponseWorker with all dependencies.
func NewGenerateResponseWorker(
	messages MessageRepository,
	sessions SessionRepository,
	router aicore.ModelRouter,
	registry aicore.PromptRegistry,
) *GenerateResponseWorker {
	return &GenerateResponseWorker{
		messages: messages,
		sessions: sessions,
		router:   router,
		registry: registry,
	}
}

func (w *GenerateResponseWorker) Work(ctx context.Context, job *river.Job[GenerateResponseArgs]) error {
	args := job.Args
	tctx := tenant.WithTenantID(ctx, args.TenantID.String())

	// Idempotency check: if this job already produced a message, skip (NFR-4/NFR-15).
	existing, err := w.messages.GetByJobID(tctx, job.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if existing != nil {
		return nil
	}

	// FSM guard: skip LLM call if session reached a terminal state since job enqueue.
	session, err := w.sessions.GetByID(tctx, args.SessionID)
	if err != nil {
		return err
	}
	if session.State == StateCompleted || session.State == StateExpired {
		return nil
	}

	// Load conversation history with a 4096-token budget (FR-23).
	history, err := w.messages.ListForSession(tctx, args.SessionID, 4096)
	if err != nil {
		return err
	}

	// Load system prompt; missing registry entry is not an error (FR-14).
	systemPrompt, err := w.registry.LoadLatest(tctx, "default")
	if err != nil && !errors.Is(err, aicore.ErrPromptNotFound) {
		return err
	}

	// Build LLM message list; sanitize only user content — not assistant/system (NFR-17).
	var msgs []aicore.Message
	if systemPrompt != "" {
		msgs = append(msgs, aicore.Message{Role: "system", Content: systemPrompt})
	}
	for _, m := range history {
		content := m.Content
		if m.Role == "user" {
			content = aicore.SanitizeInput(content)
		}
		msgs = append(msgs, aicore.Message{Role: m.Role, Content: content})
	}

	// Call LLM via router (FR-13). No DB transaction is open here (NFR-7).
	resp, err := w.router.Generate(tctx, aicore.GenerateRequest{
		Messages: msgs,
		TaskHint: aicore.TaskGeneration,
	})
	if err != nil {
		return err
	}

	// Save assistant message, linked to this job for idempotency (NFR-4).
	tokens := resp.OutputTokens
	jobID := job.ID
	msg := &Message{
		ID:         uuid.New(),
		SessionID:  args.SessionID,
		TenantID:   args.TenantID,
		Role:       "assistant",
		Content:    resp.Content,
		JobID:      &jobID,
		TokenCount: &tokens,
	}
	if err := w.messages.Create(tctx, msg); err != nil {
		return err
	}

	// Log cost for per-tenant attribution (NFR-13).
	obs.LogRequestCost(tctx, args.TenantID.String(), resp.Model, resp.InputTokens, resp.OutputTokens)

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
