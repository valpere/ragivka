package orchestrator_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/valpere/ragivka/pkg/aicore"
	"github.com/valpere/ragivka/pkg/knowledge/retrieval"
	"github.com/valpere/ragivka/pkg/orchestrator"
	"github.com/valpere/ragivka/pkg/runtime"
	"github.com/valpere/ragivka/pkg/tenant"
)

// ---------------------------------------------------------------------------
// Mock aicore.ModelRouter
// ---------------------------------------------------------------------------

type mockRouter struct {
	response string
	err      error
}

func (m *mockRouter) Generate(_ context.Context, _ aicore.GenerateRequest) (aicore.GenerateResponse, error) {
	if m.err != nil {
		return aicore.GenerateResponse{}, m.err
	}
	return aicore.GenerateResponse{Content: m.response}, nil
}

// ---------------------------------------------------------------------------
// Mock SessionRepository
// ---------------------------------------------------------------------------

type mockSessions struct {
	mu       sync.Mutex
	sessions map[uuid.UUID]*runtime.Session
}

func newMockSessions(initial ...*runtime.Session) *mockSessions {
	m := &mockSessions{sessions: make(map[uuid.UUID]*runtime.Session)}
	for _, s := range initial {
		m.sessions[s.ID] = s
	}
	return m
}

func (m *mockSessions) GetByID(_ context.Context, id uuid.UUID) (*runtime.Session, error) {
	m.mu.Lock(); defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, runtime.ErrNotFound
	}
	cp := *s
	return &cp, nil
}

func (m *mockSessions) Create(_ context.Context, s *runtime.Session) error {
	m.mu.Lock(); defer m.mu.Unlock()
	cp := *s; m.sessions[cp.ID] = &cp; return nil
}
func (m *mockSessions) GetActiveByUserID(_ context.Context, _ uuid.UUID) (*runtime.Session, error) {
	return nil, runtime.ErrNotFound
}
func (m *mockSessions) Transition(_ context.Context, _ uuid.UUID, _, _ runtime.State, _ int) (int, error) {
	return 1, nil
}
func (m *mockSessions) ListExpired(_ context.Context) ([]*runtime.Session, error) { return nil, nil }

// ---------------------------------------------------------------------------
// Mock MessageRepository
// ---------------------------------------------------------------------------

type mockMessages struct {
	mu       sync.Mutex
	messages []*runtime.Message
}

func (m *mockMessages) Create(_ context.Context, msg *runtime.Message) error {
	m.mu.Lock(); defer m.mu.Unlock()
	cp := *msg; m.messages = append(m.messages, &cp); return nil
}

func (m *mockMessages) ListForSession(_ context.Context, sessID uuid.UUID, _ int) ([]*runtime.Message, error) {
	m.mu.Lock(); defer m.mu.Unlock()
	var out []*runtime.Message
	for _, msg := range m.messages {
		if msg.SessionID == sessID {
			cp := *msg; out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *mockMessages) GetByJobID(_ context.Context, _ int64) (*runtime.Message, error) {
	return nil, runtime.ErrNotFound
}

// ---------------------------------------------------------------------------
// Mock retrieval.Retriever
// ---------------------------------------------------------------------------

type mockRetriever struct {
	chunks []retrieval.RankedChunk
	err    error
}

func (r *mockRetriever) Retrieve(_ context.Context, _ string, _ int, _ float64) ([]retrieval.RankedChunk, error) {
	return r.chunks, r.err
}

// ---------------------------------------------------------------------------
// Mock JobEnqueuer
// ---------------------------------------------------------------------------

type mockEnqueuer struct {
	enqueued []runtime.GenerateResponseArgs
	err      error
}

func (e *mockEnqueuer) EnqueueGenerateResponse(_ context.Context, args runtime.GenerateResponseArgs) error {
	if e.err != nil {
		return e.err
	}
	e.enqueued = append(e.enqueued, args)
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func tenantSession(tier runtime.Tier) *runtime.Session {
	tenantID := uuid.New()
	return &runtime.Session{
		ID:       uuid.New(),
		TenantID: tenantID,
		State:    runtime.StateActive,
		Tier:     tier,
		Version:  1,
	}
}

func tenantCtxFor(s *runtime.Session) context.Context {
	return tenant.WithTenantID(context.Background(), s.TenantID.String())
}

// ---------------------------------------------------------------------------
// L0Handler tests
// ---------------------------------------------------------------------------

func TestL0Handler_Handle_savesUserAndAssistantMessages(t *testing.T) {
	sess := tenantSession(runtime.TierL0)
	sessions := newMockSessions(sess)
	messages := &mockMessages{}
	router := &mockRouter{response: "hello from L0"}

	h := orchestrator.NewL0Handler(router, sessions, messages)
	if err := h.Handle(tenantCtxFor(sess), sess, "what is 2+2?"); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	messages.mu.Lock()
	msgs := messages.messages
	messages.mu.Unlock()

	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (user + assistant), got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("first message role: got %q, want user", msgs[0].Role)
	}
	if msgs[1].Role != "assistant" {
		t.Errorf("second message role: got %q, want assistant", msgs[1].Role)
	}
	if msgs[1].Content != "hello from L0" {
		t.Errorf("assistant content: got %q, want %q", msgs[1].Content, "hello from L0")
	}
}

func TestL0Handler_Handle_routerErrorPropagates(t *testing.T) {
	sess := tenantSession(runtime.TierL0)
	h := orchestrator.NewL0Handler(
		&mockRouter{err: errors.New("llm: timeout")},
		newMockSessions(sess),
		&mockMessages{},
	)
	if err := h.Handle(tenantCtxFor(sess), sess, "test"); err == nil {
		t.Error("expected error when router fails, got nil")
	}
}

// ---------------------------------------------------------------------------
// L1Handler tests
// ---------------------------------------------------------------------------

func TestL1Handler_Handle_savesMessagesWithCitations(t *testing.T) {
	sess := tenantSession(runtime.TierL1)
	messages := &mockMessages{}
	chunkID := uuid.New()
	retriever := &mockRetriever{chunks: []retrieval.RankedChunk{
		{ChunkID: chunkID, Content: "Paris is the capital of France."},
	}}

	h := orchestrator.NewL1Handler(
		&mockRouter{response: "Paris is the capital."},
		newMockSessions(sess),
		messages,
		retriever,
	)
	if err := h.Handle(tenantCtxFor(sess), sess, "What is the capital of France?"); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	messages.mu.Lock()
	msgs := messages.messages
	messages.mu.Unlock()

	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	assistant := msgs[1]
	if assistant.Role != "assistant" {
		t.Errorf("second message role: got %q, want assistant", assistant.Role)
	}
	if len(assistant.CitationRefs) == 0 {
		t.Error("assistant message must include CitationRefs from retrieved chunks")
	}
	if assistant.CitationRefs[0] != chunkID {
		t.Errorf("CitationRefs[0]: got %v, want %v", assistant.CitationRefs[0], chunkID)
	}
}

func TestL1Handler_Handle_retrieverErrorPropagates(t *testing.T) {
	sess := tenantSession(runtime.TierL1)
	h := orchestrator.NewL1Handler(
		&mockRouter{response: "ok"},
		newMockSessions(sess),
		&mockMessages{},
		&mockRetriever{err: errors.New("retriever: db error")},
	)
	if err := h.Handle(tenantCtxFor(sess), sess, "test"); err == nil {
		t.Error("expected error when retriever fails, got nil")
	}
}

// ---------------------------------------------------------------------------
// L2Handler tests
// ---------------------------------------------------------------------------

func TestL2Handler_Handle_savesUserMessageAndEnqueues(t *testing.T) {
	sess := tenantSession(runtime.TierL2)
	messages := &mockMessages{}
	enqueuer := &mockEnqueuer{}

	h := orchestrator.NewL2Handler(nil, newMockSessions(sess), messages, enqueuer)
	if err := h.Handle(tenantCtxFor(sess), sess, "start workflow"); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	messages.mu.Lock()
	msgCount := len(messages.messages)
	messages.mu.Unlock()

	if msgCount != 1 {
		t.Errorf("expected 1 user message saved, got %d", msgCount)
	}
	if len(enqueuer.enqueued) != 1 {
		t.Fatalf("expected 1 River job enqueued, got %d", len(enqueuer.enqueued))
	}
	job := enqueuer.enqueued[0]
	if job.SessionID != sess.ID {
		t.Errorf("enqueued job SessionID: got %v, want %v", job.SessionID, sess.ID)
	}
	if job.IdempotencyKey == "" {
		t.Error("IdempotencyKey must be non-empty")
	}
}

func TestL2Handler_Handle_enqueueErrorPropagates(t *testing.T) {
	sess := tenantSession(runtime.TierL2)
	h := orchestrator.NewL2Handler(
		nil, newMockSessions(sess), &mockMessages{},
		&mockEnqueuer{err: errors.New("river: unavailable")},
	)
	if err := h.Handle(tenantCtxFor(sess), sess, "test"); err == nil {
		t.Error("expected error when enqueue fails, got nil")
	}
}

// ---------------------------------------------------------------------------
// TieredOrchestrator tests
// ---------------------------------------------------------------------------

func TestOrchestrator_Run_dispatchesByTier(t *testing.T) {
	for _, tc := range []struct {
		tier    runtime.Tier
		wantMsg int // expected saved messages count
	}{
		{runtime.TierL0, 2}, // user + assistant
		{runtime.TierL1, 2}, // user + assistant with citations
		{runtime.TierL2, 1}, // user only (async)
	} {
		t.Run(string(tc.tier), func(t *testing.T) {
			sess := tenantSession(tc.tier)
			sessions := newMockSessions(sess)
			messages := &mockMessages{}
			router := &mockRouter{response: "ok"}
			retriever := &mockRetriever{chunks: []retrieval.RankedChunk{{ChunkID: uuid.New(), Content: "ctx"}}}
			enqueuer := &mockEnqueuer{}

			orch := orchestrator.NewTieredOrchestrator(
				sessions,
				orchestrator.NewL0Handler(router, sessions, messages),
				orchestrator.NewL1Handler(router, sessions, messages, retriever),
				orchestrator.NewL2Handler(nil, sessions, messages, enqueuer),
			)

			ctx := tenant.WithTenantID(context.Background(), sess.TenantID.String())
			if err := orch.Run(ctx, sess.ID, "hello"); err != nil {
				t.Fatalf("Run: %v", err)
			}
			messages.mu.Lock()
			count := len(messages.messages)
			messages.mu.Unlock()
			if count != tc.wantMsg {
				t.Errorf("tier %s: expected %d messages, got %d", tc.tier, tc.wantMsg, count)
			}
		})
	}
}

func TestOrchestrator_Run_unknownSessionReturnsError(t *testing.T) {
	sessions := newMockSessions()
	messages := &mockMessages{}
	router := &mockRouter{}
	orch := orchestrator.NewTieredOrchestrator(
		sessions,
		orchestrator.NewL0Handler(router, sessions, messages),
		orchestrator.NewL1Handler(router, sessions, messages, &mockRetriever{}),
		orchestrator.NewL2Handler(nil, sessions, messages, &mockEnqueuer{}),
	)
	ctx := tenant.WithTenantID(context.Background(), uuid.New().String())
	if err := orch.Run(ctx, uuid.New(), "test"); err == nil {
		t.Error("expected error for unknown session, got nil")
	}
}

// ---------------------------------------------------------------------------
// HTTP handler tests
// ---------------------------------------------------------------------------

func TestMessageHandler_invalidMethod(t *testing.T) {
	orch := &stubOrchestrator{}
	h := orchestrator.NewMessageHandler(orch)
	r := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+uuid.New().String()+"/messages", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestMessageHandler_invalidSessionID(t *testing.T) {
	h := orchestrator.NewMessageHandler(&stubOrchestrator{})
	r := httptest.NewRequest(http.MethodPost, "/v1/sessions/not-a-uuid/messages",
		strings.NewReader(`{"message":"hi"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestMessageHandler_validRequest_returns202(t *testing.T) {
	h := orchestrator.NewMessageHandler(&stubOrchestrator{})
	body := strings.NewReader(`{"message":"hello"}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/sessions/"+uuid.New().String()+"/messages", body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", w.Code)
	}
}

func TestMessageHandler_orchestratorError_returns500(t *testing.T) {
	h := orchestrator.NewMessageHandler(&stubOrchestrator{err: errors.New("session expired")})
	body := strings.NewReader(`{"message":"hi"}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/sessions/"+uuid.New().String()+"/messages", body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

type stubOrchestrator struct {
	err error
}

func (s *stubOrchestrator) Run(_ context.Context, _ uuid.UUID, _ string) error {
	// Simulate some work.
	time.Sleep(0)
	return s.err
}
