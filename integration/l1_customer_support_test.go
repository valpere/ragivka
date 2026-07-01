// Package integration exercises the full L1 Customer Support Assistant
// flow end-to-end (Issue #23, Case Study 3): Telegram webhook → session
// resolution → L1Handler (hybrid retrieval → structured output → HITL gate)
// → Telegram reply.
//
// This composes the real production types exactly as cmd/server/main.go
// wires them, substituting mocks only for external infrastructure
// (Postgres, Redis, Ollama, the live Telegram Bot API) — consistent with
// how the rest of this repo tests handler composition (see
// pkg/orchestrator/orchestrator_test.go). No live services are required to
// run this test.
package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/valpere/ragivka/pkg/aicore"
	"github.com/valpere/ragivka/pkg/channel/telegram"
	"github.com/valpere/ragivka/pkg/knowledge/retrieval"
	"github.com/valpere/ragivka/pkg/orchestrator"
	"github.com/valpere/ragivka/pkg/runtime"
	"github.com/valpere/ragivka/pkg/tools"
)

// ---------------------------------------------------------------------------
// In-memory fakes for external infrastructure
// ---------------------------------------------------------------------------

type fakeUsers struct {
	mu  sync.Mutex
	ids map[string]uuid.UUID
}

func newFakeUsers() *fakeUsers { return &fakeUsers{ids: make(map[string]uuid.UUID)} }

func (f *fakeUsers) ResolveOrCreate(_ context.Context, _, channelID string) (uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id, ok := f.ids[channelID]; ok {
		return id, nil
	}
	id := uuid.New()
	f.ids[channelID] = id
	return id, nil
}

type fakeSessions struct {
	mu     sync.Mutex
	byID   map[uuid.UUID]*runtime.Session
	byUser map[uuid.UUID]*runtime.Session
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{
		byID:   make(map[uuid.UUID]*runtime.Session),
		byUser: make(map[uuid.UUID]*runtime.Session),
	}
}

func (f *fakeSessions) Create(_ context.Context, s *runtime.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *s
	f.byID[cp.ID] = &cp
	f.byUser[cp.UserID] = &cp
	return nil
}
func (f *fakeSessions) GetByID(_ context.Context, id uuid.UUID) (*runtime.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.byID[id]
	if !ok {
		return nil, runtime.ErrNotFound
	}
	cp := *s
	return &cp, nil
}
func (f *fakeSessions) GetActiveByUserID(_ context.Context, userID uuid.UUID) (*runtime.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.byUser[userID]
	if !ok {
		return nil, runtime.ErrNotFound
	}
	cp := *s
	return &cp, nil
}
func (f *fakeSessions) Transition(_ context.Context, id uuid.UUID, _, to runtime.State, _ int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.byID[id]
	if !ok {
		return 0, runtime.ErrNotFound
	}
	s.State = to
	s.Version++
	return s.Version, nil
}
func (f *fakeSessions) ListExpired(_ context.Context) ([]*runtime.Session, error) { return nil, nil }

type fakeMessages struct {
	mu       sync.Mutex
	messages []*runtime.Message
}

func (f *fakeMessages) Create(_ context.Context, m *runtime.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *m
	f.messages = append(f.messages, &cp)
	return nil
}
func (f *fakeMessages) ListForSession(_ context.Context, sessionID uuid.UUID, _ int) ([]*runtime.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*runtime.Message
	for _, m := range f.messages {
		if m.SessionID == sessionID {
			cp := *m
			out = append(out, &cp)
		}
	}
	return out, nil
}
func (f *fakeMessages) GetByJobID(_ context.Context, _ int64) (*runtime.Message, error) {
	return nil, runtime.ErrNotFound
}

// fakeRetriever stands in for the pgvector/tsvector hybrid retriever backed
// by the migrations/006_seed_mvp.sql fixture data — same shape (a chunk with
// a stable UUID and FAQ-style content), without requiring a live Postgres.
type fakeRetriever struct {
	chunks []retrieval.RankedChunk
}

func (f *fakeRetriever) Retrieve(_ context.Context, _ string, _ int, _ float64) ([]retrieval.RankedChunk, error) {
	return f.chunks, nil
}

// fakeRouter stands in for the Ollama-backed ModelRouter. It echoes the
// retrieved context back as a structured, high-confidence answer, mirroring
// what a real grounded LLM response looks like (FR-15).
type fakeRouter struct{}

func (fakeRouter) Generate(_ context.Context, _ aicore.GenerateRequest) (aicore.GenerateResponse, error) {
	body, _ := json.Marshal(map[string]any{
		"answer":         "You can return unopened items within 30 days for a full refund.",
		"confidence":     0.92,
		"requires_human": false,
	})
	return aicore.GenerateResponse{Content: string(body)}, nil
}

type sentMessage struct {
	chatID int64
	text   string
}

type fakeSender struct {
	mu   sync.Mutex
	sent []sentMessage
}

func (f *fakeSender) SendMessage(_ context.Context, chatID int64, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentMessage{chatID: chatID, text: text})
	return nil
}

// ---------------------------------------------------------------------------
// Test
// ---------------------------------------------------------------------------

func TestL1CustomerSupportFlow(t *testing.T) {
	tenantID := uuid.New()
	chunkID := uuid.New()

	users := newFakeUsers()
	sessions := newFakeSessions()
	messages := &fakeMessages{}
	retriever := &fakeRetriever{chunks: []retrieval.RankedChunk{
		{
			ChunkID:   chunkID,
			Content:   "You can return any unopened item within 30 days of delivery for a full refund.",
			VecScore:  0.0, // seed fixture chunks have NULL embeddings (see 006_seed_mvp.sql)
			TextScore: 0.8,
			Score:     0.24,
		},
	}}
	sender := &fakeSender{}

	hitl := tools.NewHITLGate(0.7) // FR-18 default threshold
	orch := orchestrator.NewTieredOrchestrator(
		sessions,
		orchestrator.NewL0Handler(fakeRouter{}, sessions, messages),
		orchestrator.NewL1Handler(fakeRouter{}, sessions, messages, retriever, hitl),
		orchestrator.NewL2Handler(sessions, messages, nil), // L2 not exercised by this flow
	)

	const webhookSecret = "test-secret"
	handler := http.NewServeMux()
	handler.Handle("POST /telegram/webhook/{tenantID}", requireSecret(webhookSecret,
		telegram.NewWebhookHandler(users, sessions, messages, orch, sender, 0),
	))

	srv := httptest.NewServer(handler)
	defer srv.Close()

	// Seed an Active L1 session directly (bypasses the "first contact creates
	// L0" path so this test exercises the RAG flow, matching Case Study 3's
	// production tier assignment for a customer-support conversation).
	userID, err := users.ResolveOrCreate(context.Background(), "telegram", "555000111")
	if err != nil {
		t.Fatalf("ResolveOrCreate: %v", err)
	}
	sess := &runtime.Session{
		ID: uuid.New(), TenantID: tenantID, UserID: userID,
		State: runtime.StateActive, Tier: runtime.TierL1, Channel: "telegram", Version: 1,
	}
	if err := sessions.Create(context.Background(), sess); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"update_id": 1,
		"message": map[string]any{
			"message_id": 1,
			"from":       map[string]any{"id": 555000111},
			"chat":       map[string]any{"id": 555000111},
			"text":       "What is your return policy?",
		},
	})

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/telegram/webhook/"+tenantID.String(), strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", webhookSecret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("webhook request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}

	// Assert: a reply was sent back to the correct Telegram chat.
	sender.mu.Lock()
	sentCount := len(sender.sent)
	var reply sentMessage
	if sentCount > 0 {
		reply = sender.sent[0]
	}
	sender.mu.Unlock()
	if sentCount != 1 {
		t.Fatalf("expected 1 Telegram reply, got %d", sentCount)
	}
	if reply.chatID != 555000111 {
		t.Errorf("reply chatID: got %d, want 555000111", reply.chatID)
	}
	if !strings.Contains(reply.text, "30 days") {
		t.Errorf("reply text does not reflect the grounded answer: %q", reply.text)
	}

	// Assert: the persisted assistant message carries the real chunk UUID as
	// a citation (FR-12) — traceable back to the source content, not just a
	// plausible-looking answer.
	messages.mu.Lock()
	defer messages.mu.Unlock()
	var assistant *runtime.Message
	for _, m := range messages.messages {
		if m.Role == "assistant" {
			assistant = m
		}
	}
	if assistant == nil {
		t.Fatal("no assistant message was persisted")
	}
	if len(assistant.CitationRefs) != 1 || assistant.CitationRefs[0] != chunkID {
		t.Errorf("CitationRefs: got %v, want [%v]", assistant.CitationRefs, chunkID)
	}
}

// requireSecret is a minimal stand-in for middleware.TelegramSecretAuth,
// avoiding a dependency on pkg/middleware for this single check.
func requireSecret(secret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Telegram-Bot-Api-Secret-Token") != secret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
