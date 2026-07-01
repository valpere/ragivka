package telegram_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/valpere/ragivka/pkg/channel/telegram"
	"github.com/valpere/ragivka/pkg/runtime"
)

// ---------------------------------------------------------------------------
// Mock SessionRepository
// ---------------------------------------------------------------------------

type mockSessions struct {
	mu           sync.Mutex
	byUser       map[uuid.UUID]*runtime.Session
	created      []*runtime.Session
	getByUserErr error
}

func newMockSessions() *mockSessions {
	return &mockSessions{byUser: make(map[uuid.UUID]*runtime.Session)}
}

func (m *mockSessions) Create(_ context.Context, s *runtime.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	m.byUser[cp.UserID] = &cp
	m.created = append(m.created, &cp)
	return nil
}
func (m *mockSessions) GetByID(_ context.Context, _ uuid.UUID) (*runtime.Session, error) {
	return nil, runtime.ErrNotFound
}
func (m *mockSessions) GetActiveByUserID(_ context.Context, userID uuid.UUID) (*runtime.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getByUserErr != nil {
		return nil, m.getByUserErr
	}
	s, ok := m.byUser[userID]
	if !ok {
		return nil, runtime.ErrNotFound
	}
	cp := *s
	return &cp, nil
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
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *msg
	m.messages = append(m.messages, &cp)
	return nil
}
func (m *mockMessages) ListForSession(_ context.Context, sessID uuid.UUID, _ int) ([]*runtime.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*runtime.Message
	for _, msg := range m.messages {
		if msg.SessionID == sessID {
			cp := *msg
			out = append(out, &cp)
		}
	}
	return out, nil
}
func (m *mockMessages) GetByJobID(_ context.Context, _ int64) (*runtime.Message, error) {
	return nil, runtime.ErrNotFound
}

// ---------------------------------------------------------------------------
// Mock Orchestrator
// ---------------------------------------------------------------------------

type mockOrchestrator struct {
	err error
	// onRun lets a test simulate the assistant reply that a real Handle
	// would have persisted before Run returns.
	onRun func(sessionID uuid.UUID)
	calls []uuid.UUID
}

func (o *mockOrchestrator) Run(_ context.Context, sessionID uuid.UUID, _ string) error {
	o.calls = append(o.calls, sessionID)
	if o.err != nil {
		return o.err
	}
	if o.onRun != nil {
		o.onRun(sessionID)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Mock Sender
// ---------------------------------------------------------------------------

type mockSender struct {
	mu   sync.Mutex
	sent []sentMsg
	err  error
}
type sentMsg struct {
	chatID int64
	text   string
}

func (s *mockSender) SendMessage(_ context.Context, chatID int64, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.sent = append(s.sent, sentMsg{chatID: chatID, text: text})
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func telegramUpdateBody(userID, chatID int64, text string) *bytes.Reader {
	body, _ := json.Marshal(map[string]any{
		"update_id": 1,
		"message": map[string]any{
			"message_id": 1,
			"from":       map[string]any{"id": userID},
			"chat":       map[string]any{"id": chatID},
			"text":       text,
		},
	})
	return bytes.NewReader(body)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestWebhookHandler_createsSessionAndRepliesForSyncTier(t *testing.T) {
	tenantID := uuid.New()
	sessions := newMockSessions()
	messages := &mockMessages{}
	orch := &mockOrchestrator{onRun: func(sessionID uuid.UUID) {
		_ = messages.Create(context.Background(), &runtime.Message{
			ID: uuid.New(), SessionID: sessionID, Role: "assistant", Content: "hello human",
		})
	}}
	sender := &mockSender{}

	h := telegram.NewWebhookHandler(sessions, messages, orch, sender, 0)
	r := httptest.NewRequest(http.MethodPost, "/telegram/webhook/"+tenantID.String(),
		telegramUpdateBody(555, 999, "hi bot"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	sessions.mu.Lock()
	createdCount := len(sessions.created)
	sessions.mu.Unlock()
	if createdCount != 1 {
		t.Fatalf("expected 1 session created, got %d", createdCount)
	}
	if len(orch.calls) != 1 {
		t.Fatalf("expected orchestrator.Run called once, got %d", len(orch.calls))
	}

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.sent) != 1 {
		t.Fatalf("expected 1 reply sent, got %d", len(sender.sent))
	}
	if sender.sent[0].chatID != 999 {
		t.Errorf("chatID: got %d, want 999", sender.sent[0].chatID)
	}
	if sender.sent[0].text != "hello human" {
		t.Errorf("text: got %q, want %q", sender.sent[0].text, "hello human")
	}
}

func TestWebhookHandler_reusesExistingSessionForSameUser(t *testing.T) {
	tenantID := uuid.New()
	sessions := newMockSessions()
	messages := &mockMessages{}
	orch := &mockOrchestrator{}
	sender := &mockSender{}
	h := telegram.NewWebhookHandler(sessions, messages, orch, sender, 0)

	r1 := httptest.NewRequest(http.MethodPost, "/telegram/webhook/"+tenantID.String(),
		telegramUpdateBody(555, 999, "first"))
	h.ServeHTTP(httptest.NewRecorder(), r1)

	r2 := httptest.NewRequest(http.MethodPost, "/telegram/webhook/"+tenantID.String(),
		telegramUpdateBody(555, 999, "second"))
	h.ServeHTTP(httptest.NewRecorder(), r2)

	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	if len(sessions.created) != 1 {
		t.Errorf("expected exactly 1 session created across 2 updates from same user, got %d", len(sessions.created))
	}
	if len(orch.calls) != 2 {
		t.Errorf("expected orchestrator.Run called twice, got %d", len(orch.calls))
	}
	if orch.calls[0] != orch.calls[1] {
		t.Errorf("both updates must route to the same session: %v vs %v", orch.calls[0], orch.calls[1])
	}
}

func TestWebhookHandler_asyncTier_doesNotSendReply(t *testing.T) {
	tenantID := uuid.New()
	sessions := newMockSessions()
	// Pre-seed an L2 session so resolveOrCreateSession finds it directly.
	userID := uuid.NewSHA1(uuid.MustParse("6f6d0f2e-6f6c-4a7c-9f3e-3d6c1f2b9a11"), []byte("telegram:555"))
	sessions.byUser[userID] = &runtime.Session{
		ID: uuid.New(), TenantID: tenantID, UserID: userID, State: runtime.StateActive, Tier: runtime.TierL2,
	}
	messages := &mockMessages{}
	orch := &mockOrchestrator{}
	sender := &mockSender{}

	h := telegram.NewWebhookHandler(sessions, messages, orch, sender, 0)
	r := httptest.NewRequest(http.MethodPost, "/telegram/webhook/"+tenantID.String(),
		telegramUpdateBody(555, 999, "async please"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.sent) != 0 {
		t.Errorf("expected no synchronous reply for L2 tier, got %d", len(sender.sent))
	}
}

func TestWebhookHandler_nonTextUpdate_returns200WithoutOrchestratorCall(t *testing.T) {
	tenantID := uuid.New()
	sessions := newMockSessions()
	messages := &mockMessages{}
	orch := &mockOrchestrator{}
	sender := &mockSender{}

	h := telegram.NewWebhookHandler(sessions, messages, orch, sender, 0)
	body, _ := json.Marshal(map[string]any{"update_id": 1}) // no message field
	r := httptest.NewRequest(http.MethodPost, "/telegram/webhook/"+tenantID.String(), bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}
	if len(orch.calls) != 0 {
		t.Errorf("expected orchestrator not called for non-text update, got %d calls", len(orch.calls))
	}
}

func TestWebhookHandler_invalidTenantID_returns400(t *testing.T) {
	h := telegram.NewWebhookHandler(newMockSessions(), &mockMessages{}, &mockOrchestrator{}, &mockSender{}, 0)
	r := httptest.NewRequest(http.MethodPost, "/telegram/webhook/not-a-uuid", telegramUpdateBody(1, 1, "hi"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", w.Code)
	}
}

func TestWebhookHandler_wrongMethod_returns405(t *testing.T) {
	h := telegram.NewWebhookHandler(newMockSessions(), &mockMessages{}, &mockOrchestrator{}, &mockSender{}, 0)
	r := httptest.NewRequest(http.MethodGet, "/telegram/webhook/"+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want 405", w.Code)
	}
}

func TestWebhookHandler_malformedJSON_returns400(t *testing.T) {
	h := telegram.NewWebhookHandler(newMockSessions(), &mockMessages{}, &mockOrchestrator{}, &mockSender{}, 0)
	r := httptest.NewRequest(http.MethodPost, "/telegram/webhook/"+uuid.New().String(), strings.NewReader(`{not json`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", w.Code)
	}
}

func TestWebhookHandler_orchestratorError_returns500(t *testing.T) {
	orch := &mockOrchestrator{err: errors.New("llm: timeout")}
	h := telegram.NewWebhookHandler(newMockSessions(), &mockMessages{}, orch, &mockSender{}, 0)
	r := httptest.NewRequest(http.MethodPost, "/telegram/webhook/"+uuid.New().String(),
		telegramUpdateBody(1, 1, "hi"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", w.Code)
	}
}

func TestWebhookHandler_sessionLookupError_returns500(t *testing.T) {
	sessions := newMockSessions()
	sessions.getByUserErr = errors.New("db: connection refused")
	h := telegram.NewWebhookHandler(sessions, &mockMessages{}, &mockOrchestrator{}, &mockSender{}, 0)
	r := httptest.NewRequest(http.MethodPost, "/telegram/webhook/"+uuid.New().String(),
		telegramUpdateBody(1, 1, "hi"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", w.Code)
	}
}

// ---------------------------------------------------------------------------
// HTTPSender
// ---------------------------------------------------------------------------

func TestHTTPSender_sendMessage_postsToTelegramAPI(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()

	sender := telegram.NewHTTPSenderForTest(ts.URL, "TESTTOKEN")
	if err := sender.SendMessage(context.Background(), 12345, "hello"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if gotPath != "/botTESTTOKEN/sendMessage" {
		t.Errorf("path: got %q, want /botTESTTOKEN/sendMessage", gotPath)
	}
	if gotBody["chat_id"] != float64(12345) {
		t.Errorf("chat_id: got %v, want 12345", gotBody["chat_id"])
	}
	if gotBody["text"] != "hello" {
		t.Errorf("text: got %v, want hello", gotBody["text"])
	}
}

func TestHTTPSender_nonOKStatus_returnsError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()

	sender := telegram.NewHTTPSenderForTest(ts.URL, "TESTTOKEN")
	if err := sender.SendMessage(context.Background(), 1, "hi"); err == nil {
		t.Error("expected error for non-200 response, got nil")
	}
}
