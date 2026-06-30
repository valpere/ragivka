package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/valpere/ragivka/pkg/aicore"
	"github.com/valpere/ragivka/pkg/tenant"
)

// ======================== Mock SessionRepository ========================

// mockSessionRepo implements SessionRepository with in-memory state.
// Transition calls are recorded so tests can inspect the context used.
type mockSessionRepo struct {
	sessions       map[uuid.UUID]*Session
	transitionCtxs []context.Context // contexts passed to each Transition call
}

func newMockSessionRepo(initial ...*Session) *mockSessionRepo {
	r := &mockSessionRepo{sessions: make(map[uuid.UUID]*Session)}
	for _, s := range initial {
		cp := *s
		r.sessions[cp.ID] = &cp
	}
	return r
}

func (r *mockSessionRepo) Create(_ context.Context, s *Session) error {
	cp := *s
	r.sessions[cp.ID] = &cp
	return nil
}

func (r *mockSessionRepo) GetByID(_ context.Context, id uuid.UUID) (*Session, error) {
	s, ok := r.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *s
	return &cp, nil
}

func (r *mockSessionRepo) GetActiveByUserID(_ context.Context, userID uuid.UUID) (*Session, error) {
	for _, s := range r.sessions {
		if s.UserID == userID && (s.State == StateActive || s.State == StateWaitingForHuman) {
			cp := *s
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

// Transition mirrors the real pgSessionRepo: validates the FSM edge, then
// enforces optimistic-lock version matching.
func (r *mockSessionRepo) Transition(ctx context.Context, id uuid.UUID, from, to State, version int) (int, error) {
	r.transitionCtxs = append(r.transitionCtxs, ctx)
	if !Allowed(from, to) {
		return 0, ErrInvalidTransition{From: from, To: to}
	}
	s, ok := r.sessions[id]
	if !ok {
		return 0, ErrNotFound
	}
	if s.Version != version {
		return 0, ErrOptimisticLock
	}
	s.State = to
	s.Version++
	return s.Version, nil
}

// ListExpired returns sessions that are still in a non-terminal state.
// Time-based filtering is omitted; the mock treats all such sessions as expired.
func (r *mockSessionRepo) ListExpired(_ context.Context) ([]*Session, error) {
	var result []*Session
	for _, s := range r.sessions {
		if s.State == StateActive || s.State == StateWaitingForHuman {
			cp := *s
			result = append(result, &cp)
		}
	}
	return result, nil
}

// ======================== Mock MessageRepository ========================

// mockMessageRepo implements MessageRepository with an in-memory message slice.
// ListForSession replicates the token-budget algorithm from pgMessageRepo (FR-23)
// so the regression tests exercise real truncation logic without a DB.
type mockMessageRepo struct {
	messages []*Message
	created  []*Message
}

func newMockMessageRepo(messages ...*Message) *mockMessageRepo {
	return &mockMessageRepo{messages: messages}
}

func (r *mockMessageRepo) Create(_ context.Context, m *Message) error {
	r.created = append(r.created, m)
	return nil
}

// ListForSession traverses messages newest-first, accumulates the token budget,
// then reverses the result to chronological order — identical logic to pgMessageRepo.
func (r *mockMessageRepo) ListForSession(_ context.Context, sessionID uuid.UUID, maxTokens int) ([]*Message, error) {
	unlimited := maxTokens <= 0
	budget := maxTokens
	var result []*Message
	for i := len(r.messages) - 1; i >= 0; i-- {
		m := r.messages[i]
		if m.SessionID != sessionID {
			continue
		}
		if !unlimited && m.TokenCount != nil {
			budget -= *m.TokenCount
			if budget < 0 {
				break
			}
		}
		result = append(result, m)
	}
	// Reverse to chronological order.
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result, nil
}

func (r *mockMessageRepo) GetByJobID(_ context.Context, jobID int64) (*Message, error) {
	for _, m := range r.messages {
		if m.JobID != nil && *m.JobID == jobID {
			return m, nil
		}
	}
	return nil, ErrNotFound
}

// ======================== Mock aicore.ModelRouter / PromptRegistry ========================

type mockRouter struct {
	resp   aicore.GenerateResponse
	err    error
	called bool
}

func (r *mockRouter) Generate(_ context.Context, _ aicore.GenerateRequest) (aicore.GenerateResponse, error) {
	r.called = true
	return r.resp, r.err
}

// mockRegistry always returns ErrPromptNotFound so GenerateResponseWorker
// treats it as "no system prompt configured" (non-fatal per FR-14).
type mockRegistry struct{}

func (r *mockRegistry) Load(_ context.Context, _ string, _ int) (string, error) {
	return "", aicore.ErrPromptNotFound
}

func (r *mockRegistry) LoadLatest(_ context.Context, _ string) (string, error) {
	return "", aicore.ErrPromptNotFound
}

// ======================== Item 1: Session FSM mock-based Transition tests ========================

// TestMockSessionRepo_Transition_ActiveToWaitingForHuman verifies the happy path:
// a session in Active state advances to WaitingForHuman and its version is incremented.
func TestMockSessionRepo_Transition_ActiveToWaitingForHuman(t *testing.T) {
	id := uuid.New()
	tenantID := uuid.New()
	repo := newMockSessionRepo(&Session{
		ID:       id,
		TenantID: tenantID,
		State:    StateActive,
		Version:  0,
	})
	ctx := tenant.WithTenantID(context.Background(), tenantID.String())

	newVer, err := repo.Transition(ctx, id, StateActive, StateWaitingForHuman, 0)
	if err != nil {
		t.Fatalf("Transition Active→WaitingForHuman: unexpected error: %v", err)
	}
	if newVer != 1 {
		t.Errorf("want new version 1, got %d", newVer)
	}
	s, _ := repo.GetByID(ctx, id)
	if s.State != StateWaitingForHuman {
		t.Errorf("want state WaitingForHuman after transition, got %s", s.State)
	}
}

// TestMockSessionRepo_Transition_ErrOptimisticLock is a regression test for PR #12:
// concurrent writes that bump the version must return ErrOptimisticLock so callers
// can skip rather than silently overwrite the newer state.
func TestMockSessionRepo_Transition_ErrOptimisticLock(t *testing.T) {
	id := uuid.New()
	tenantID := uuid.New()
	repo := newMockSessionRepo(&Session{
		ID:       id,
		TenantID: tenantID,
		State:    StateActive,
		Version:  5, // current version is 5
	})
	ctx := tenant.WithTenantID(context.Background(), tenantID.String())

	// Caller's view of version is stale (4 instead of 5).
	_, err := repo.Transition(ctx, id, StateActive, StateWaitingForHuman, 4)
	if !errors.Is(err, ErrOptimisticLock) {
		t.Errorf("stale-version Transition: want ErrOptimisticLock, got %v", err)
	}
}

// TestMockSessionRepo_Transition_TerminalStateReturnsError verifies that Completed and
// Expired sessions cannot be transitioned anywhere (terminal state = immutable).
func TestMockSessionRepo_Transition_TerminalStateReturnsError(t *testing.T) {
	tenantID := uuid.New()
	ctx := tenant.WithTenantID(context.Background(), tenantID.String())

	for _, terminalState := range []State{StateCompleted, StateExpired} {
		t.Run(string(terminalState), func(t *testing.T) {
			id := uuid.New()
			repo := newMockSessionRepo(&Session{
				ID:       id,
				TenantID: tenantID,
				State:    terminalState,
				Version:  0,
			})

			_, err := repo.Transition(ctx, id, terminalState, StateActive, 0)
			if err == nil {
				t.Fatalf("Transition from terminal state %s: want error, got nil", terminalState)
			}
			var inv ErrInvalidTransition
			if !errors.As(err, &inv) {
				t.Errorf("Transition from terminal state %s: want ErrInvalidTransition, got %T: %v",
					terminalState, err, err)
			}
		})
	}
}

// ======================== Item 2: Message repo maxTokens guard ========================

// TestMockMessageRepo_ListForSession_Unlimited is a regression test for PR #12:
// maxTokens <= 0 must return all messages without applying any token budget.
func TestMockMessageRepo_ListForSession_Unlimited(t *testing.T) {
	sessID := uuid.New()
	tc100 := 100
	tc200 := 200
	repo := newMockMessageRepo(
		&Message{ID: uuid.New(), SessionID: sessID, Role: "user", Content: "a", TokenCount: &tc100},
		&Message{ID: uuid.New(), SessionID: sessID, Role: "assistant", Content: "b", TokenCount: &tc200},
	)

	for _, limit := range []int{0, -1} {
		t.Run("maxTokens="+itoa(limit), func(t *testing.T) {
			msgs, err := repo.ListForSession(context.Background(), sessID, limit)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(msgs) != 2 {
				t.Errorf("want 2 messages for unlimited budget (maxTokens=%d), got %d", limit, len(msgs))
			}
		})
	}
}

// TestMockMessageRepo_ListForSession_TokenBudgetTruncation verifies that messages
// exceeding the cumulative token budget are excluded from the tail (newest-first pass).
// With 3×100-token messages and a 200-token budget, only the 2 newest fit.
func TestMockMessageRepo_ListForSession_TokenBudgetTruncation(t *testing.T) {
	sessID := uuid.New()
	tc100 := 100
	repo := newMockMessageRepo(
		&Message{ID: uuid.New(), SessionID: sessID, Role: "user", Content: "old", TokenCount: &tc100},
		&Message{ID: uuid.New(), SessionID: sessID, Role: "assistant", Content: "mid", TokenCount: &tc100},
		&Message{ID: uuid.New(), SessionID: sessID, Role: "user", Content: "new", TokenCount: &tc100},
	)

	msgs, err := repo.ListForSession(context.Background(), sessID, 200)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("want 2 messages within 200-token budget (3×100, newest-first), got %d", len(msgs))
	}
	for _, m := range msgs {
		if m.Content == "old" {
			t.Error("oldest message should have been excluded by token budget")
		}
	}
}

// ======================== Item 3: GenerateResponseWorker terminal session skip ========================

// TestGenerateResponseWorker_Work_TerminalSessionSkip is a regression test for PR #12:
// if a session reaches Completed or Expired between job enqueue and execution,
// Work must return nil without calling the LLM router or persisting any message.
func TestGenerateResponseWorker_Work_TerminalSessionSkip(t *testing.T) {
	for _, state := range []State{StateCompleted, StateExpired} {
		t.Run(string(state), func(t *testing.T) {
			sessID := uuid.New()
			tenantID := uuid.New()

			sessions := newMockSessionRepo(&Session{
				ID:       sessID,
				TenantID: tenantID,
				State:    state,
				Version:  1,
			})
			messages := newMockMessageRepo()
			router := &mockRouter{}
			registry := &mockRegistry{}

			worker := NewGenerateResponseWorker(messages, sessions, router, registry, nil)

			job := &river.Job[GenerateResponseArgs]{
				JobRow: &rivertype.JobRow{ID: 99},
				Args: GenerateResponseArgs{
					TenantID:       tenantID,
					SessionID:      sessID,
					MessageID:      uuid.New(),
					IdempotencyKey: "idem-1",
				},
			}

			err := worker.Work(context.Background(), job)
			if err != nil {
				t.Errorf("Work with %s session: want nil, got %v", state, err)
			}
			if router.called {
				t.Errorf("Work with %s session: router must not be called for terminal sessions", state)
			}
			if len(messages.created) > 0 {
				t.Errorf("Work with %s session: no message should be created, got %d", state, len(messages.created))
			}
		})
	}
}

// ======================== Item 4: ExpireSessionsWorker per-session tenant context ========================

// TestExpireSessionsWorker_Work_InjectsTenantContext is a regression test for PR #12:
// the expiry sweep must inject each session's own TenantID into the context before
// calling Transition, so NFR-16 cross-tenant isolation is preserved.
func TestExpireSessionsWorker_Work_InjectsTenantContext(t *testing.T) {
	tenant1ID := uuid.New()
	tenant2ID := uuid.New()
	sess1ID := uuid.New()
	sess2ID := uuid.New()

	sessions := newMockSessionRepo(
		&Session{ID: sess1ID, TenantID: tenant1ID, State: StateActive, Version: 0},
		&Session{ID: sess2ID, TenantID: tenant2ID, State: StateActive, Version: 0},
	)

	worker := NewExpireSessionsWorker(sessions)
	err := worker.Work(context.Background(), &river.Job[ExpireSessionsArgs]{
		JobRow: &rivertype.JobRow{ID: 1},
	})
	if err != nil {
		t.Fatalf("Work: unexpected error: %v", err)
	}

	if len(sessions.transitionCtxs) != 2 {
		t.Fatalf("want 2 Transition calls (one per session), got %d", len(sessions.transitionCtxs))
	}

	// Each Transition call must carry the owning session's TenantID in context.
	seenTenants := make(map[string]bool, 2)
	for _, ctx := range sessions.transitionCtxs {
		tid := tenant.MustGetTenantID(ctx)
		seenTenants[tid] = true
	}
	if !seenTenants[tenant1ID.String()] {
		t.Errorf("Transition not called with tenant1 context (%s)", tenant1ID)
	}
	if !seenTenants[tenant2ID.String()] {
		t.Errorf("Transition not called with tenant2 context (%s)", tenant2ID)
	}
}

// itoa converts an int to string for use in subtest names without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	b := make([]byte, 0, 10)
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
