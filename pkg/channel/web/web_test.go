package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/valpere/ragivka/pkg/channel/web"
	"github.com/valpere/ragivka/pkg/middleware"
	"github.com/valpere/ragivka/pkg/runtime"
	"github.com/valpere/ragivka/pkg/tenant"
)

// ---------------------------------------------------------------------------
// Mock SessionRepository
// ---------------------------------------------------------------------------

type mockSessions struct {
	mu       sync.Mutex
	sessions map[uuid.UUID]*runtime.Session
	created  []*runtime.Session
}

func newMockSessions(initial ...*runtime.Session) *mockSessions {
	m := &mockSessions{sessions: make(map[uuid.UUID]*runtime.Session)}
	for _, s := range initial {
		m.sessions[s.ID] = s
	}
	return m
}

func (m *mockSessions) Create(_ context.Context, s *runtime.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	m.sessions[cp.ID] = &cp
	m.created = append(m.created, &cp)
	return nil
}
func (m *mockSessions) GetByID(_ context.Context, id uuid.UUID) (*runtime.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, runtime.ErrNotFound
	}
	cp := *s
	return &cp, nil
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
// CreateSessionHandler
// ---------------------------------------------------------------------------

func ctxWithIdentity(tenantID, userID string) context.Context {
	ctx := tenant.WithTenantID(context.Background(), tenantID)
	return middleware.WithUserID(ctx, userID)
}

func TestCreateSessionHandler_createsSessionForAuthenticatedUser(t *testing.T) {
	sessions := newMockSessions()
	tenantID := uuid.New().String()
	userID := uuid.New().String()

	h := web.NewCreateSessionHandler(sessions, 0)
	r := httptest.NewRequest(http.MethodPost, "/v1/sessions", strings.NewReader(`{}`))
	r = r.WithContext(ctxWithIdentity(tenantID, userID))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201 (body: %s)", w.Code, w.Body.String())
	}
	var resp web.CreateSessionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SessionID == "" {
		t.Error("expected non-empty session_id")
	}

	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	if len(sessions.created) != 1 {
		t.Fatalf("expected 1 session created, got %d", len(sessions.created))
	}
	created := sessions.created[0]
	if created.TenantID.String() != tenantID {
		t.Errorf("tenant: got %v, want %v", created.TenantID, tenantID)
	}
	if created.Channel != "web" {
		t.Errorf("channel: got %q, want web", created.Channel)
	}
	if created.Tier != runtime.TierL0 {
		t.Errorf("tier: got %q, want default L0", created.Tier)
	}
}

func TestCreateSessionHandler_missingTenant_returns401(t *testing.T) {
	h := web.NewCreateSessionHandler(newMockSessions(), 0)
	r := httptest.NewRequest(http.MethodPost, "/v1/sessions", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
}

func TestCreateSessionHandler_wrongMethod_returns405(t *testing.T) {
	h := web.NewCreateSessionHandler(newMockSessions(), 0)
	r := httptest.NewRequest(http.MethodGet, "/v1/sessions", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want 405", w.Code)
	}
}

// ---------------------------------------------------------------------------
// ListMessagesHandler
// ---------------------------------------------------------------------------

func TestListMessagesHandler_returnsHistoryForOwningTenant(t *testing.T) {
	tenantID := uuid.New()
	sess := &runtime.Session{ID: uuid.New(), TenantID: tenantID, State: runtime.StateActive, Tier: runtime.TierL0}
	sessions := newMockSessions(sess)
	messages := &mockMessages{}
	_ = messages.Create(context.Background(), &runtime.Message{
		ID: uuid.New(), SessionID: sess.ID, TenantID: tenantID, Role: "user", Content: "hi", CreatedAt: time.Now(),
	})
	_ = messages.Create(context.Background(), &runtime.Message{
		ID: uuid.New(), SessionID: sess.ID, TenantID: tenantID, Role: "assistant", Content: "hello", CreatedAt: time.Now(),
	})

	h := web.NewListMessagesHandler(sessions, messages)
	r := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+sess.ID.String()+"/messages", nil)
	r = r.WithContext(tenant.WithTenantID(context.Background(), tenantID.String()))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	var views []web.MessageView
	if err := json.NewDecoder(w.Body).Decode(&views); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(views))
	}
}

func TestListMessagesHandler_crossTenantAccess_returns404(t *testing.T) {
	ownerTenant := uuid.New()
	sess := &runtime.Session{ID: uuid.New(), TenantID: ownerTenant, State: runtime.StateActive, Tier: runtime.TierL0}
	sessions := newMockSessions(sess)
	messages := &mockMessages{}

	h := web.NewListMessagesHandler(sessions, messages)
	r := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+sess.ID.String()+"/messages", nil)
	r = r.WithContext(tenant.WithTenantID(context.Background(), uuid.New().String())) // different tenant
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404 for cross-tenant access", w.Code)
	}
}

func TestListMessagesHandler_invalidSessionID_returns400(t *testing.T) {
	h := web.NewListMessagesHandler(newMockSessions(), &mockMessages{})
	r := httptest.NewRequest(http.MethodGet, "/v1/sessions/not-a-uuid/messages", nil)
	r = r.WithContext(tenant.WithTenantID(context.Background(), uuid.New().String()))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", w.Code)
	}
}

func TestListMessagesHandler_unknownSession_returns404(t *testing.T) {
	h := web.NewListMessagesHandler(newMockSessions(), &mockMessages{})
	r := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+uuid.New().String()+"/messages", nil)
	r = r.WithContext(tenant.WithTenantID(context.Background(), uuid.New().String()))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", w.Code)
	}
}

// ---------------------------------------------------------------------------
// MemoryBroadcaster
// ---------------------------------------------------------------------------

func TestMemoryBroadcaster_deliversToSubscriber(t *testing.T) {
	b := web.NewMemoryBroadcaster()
	sessionID := uuid.New()

	ch, cancel := b.Subscribe(context.Background(), sessionID)
	defer cancel()

	if err := b.Publish(context.Background(), sessionID, []byte("hello")); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case msg := <-ch:
		if string(msg) != "hello" {
			t.Errorf("received %q, want hello", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for published message")
	}
}

func TestMemoryBroadcaster_noSubscribers_publishIsNoop(t *testing.T) {
	b := web.NewMemoryBroadcaster()
	if err := b.Publish(context.Background(), uuid.New(), []byte("orphan")); err != nil {
		t.Fatalf("Publish to no subscribers must not error: %v", err)
	}
}

func TestMemoryBroadcaster_cancelStopsDelivery(t *testing.T) {
	b := web.NewMemoryBroadcaster()
	sessionID := uuid.New()

	ch, cancel := b.Subscribe(context.Background(), sessionID)
	cancel()

	if err := b.Publish(context.Background(), sessionID, []byte("late")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if _, ok := <-ch; ok {
		t.Error("expected channel closed after cancel, got open channel")
	}
}

func TestMemoryBroadcaster_isolatesSessions(t *testing.T) {
	b := web.NewMemoryBroadcaster()
	sessA, sessB := uuid.New(), uuid.New()

	chA, cancelA := b.Subscribe(context.Background(), sessA)
	defer cancelA()
	chB, cancelB := b.Subscribe(context.Background(), sessB)
	defer cancelB()

	_ = b.Publish(context.Background(), sessA, []byte("for-a"))

	select {
	case msg := <-chA:
		if string(msg) != "for-a" {
			t.Errorf("chA received %q, want for-a", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting on chA")
	}

	select {
	case msg := <-chB:
		t.Errorf("chB must not receive session A's message, got %q", msg)
	case <-time.After(50 * time.Millisecond):
		// expected: no cross-session delivery
	}
}

// ---------------------------------------------------------------------------
// WebSocketHandler
// ---------------------------------------------------------------------------

func TestWebSocketHandler_forwardsPublishedMessages(t *testing.T) {
	tenantID := uuid.New()
	sess := &runtime.Session{ID: uuid.New(), TenantID: tenantID, State: runtime.StateActive, Tier: runtime.TierL2}
	sessions := newMockSessions(sess)
	broadcaster := web.NewMemoryBroadcaster()

	handler := middlewareTenant(tenantID, web.NewWebSocketHandler(sessions, broadcaster))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/sessions/" + sess.ID.String()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Give the server a moment to register the subscription before publishing.
	time.Sleep(50 * time.Millisecond)
	if err := broadcaster.Publish(context.Background(), sess.ID, []byte(`{"role":"assistant","content":"hi"}`)); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if string(msg) != `{"role":"assistant","content":"hi"}` {
		t.Errorf("received %q, want the published payload", msg)
	}
}

func TestWebSocketHandler_unknownSession_returns404(t *testing.T) {
	tenantID := uuid.New()
	sessions := newMockSessions()
	broadcaster := web.NewMemoryBroadcaster()

	handler := middlewareTenant(tenantID, web.NewWebSocketHandler(sessions, broadcaster))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	r, err := http.Get(srv.URL + "/ws/sessions/" + uuid.New().String())
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", r.StatusCode)
	}
}

// middlewareTenant injects a fixed tenant ID into every request's context —
// stands in for JWTAuth in tests that don't need full JWT machinery.
func middlewareTenant(tenantID uuid.UUID, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := tenant.WithTenantID(r.Context(), tenantID.String())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
