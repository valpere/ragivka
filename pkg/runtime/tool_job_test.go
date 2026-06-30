package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/valpere/ragivka/pkg/tenant"
	"github.com/valpere/ragivka/pkg/tools"
)

// ---------------------------------------------------------------------------
// Tool test doubles (mockSessionRepo reused from workers_test.go)
// ---------------------------------------------------------------------------

type echoToolTJ struct {
	name string
	kind tools.ToolKind
}

func (e *echoToolTJ) Name() string                                           { return e.name }
func (e *echoToolTJ) Kind() tools.ToolKind                                   { return e.kind }
func (e *echoToolTJ) Execute(_ context.Context, a json.RawMessage) (json.RawMessage, error) {
	return a, nil
}

type mockAuditWriterTJ struct {
	executed map[string]bool
	written  []tools.AuditRecord
}

func newMockAuditWriterTJ() *mockAuditWriterTJ {
	return &mockAuditWriterTJ{executed: make(map[string]bool)}
}

func (m *mockAuditWriterTJ) IsExecuted(_ context.Context, key string) (bool, error) {
	return m.executed[key], nil
}

func (m *mockAuditWriterTJ) Write(_ context.Context, rec tools.AuditRecord) error {
	m.executed[rec.IdempotencyKey] = true
	m.written = append(m.written, rec)
	return nil
}

// ---------------------------------------------------------------------------
// Helper builders
// ---------------------------------------------------------------------------

func newToolWorker(kind tools.ToolKind, threshold float64, sess *mockSessionRepo) (*ExecuteToolWorker, *mockAuditWriterTJ) {
	reg := tools.NewRegistry()
	_ = reg.Register(&echoToolTJ{"t", kind})
	aw := newMockAuditWriterTJ()
	return NewExecuteToolWorker(reg, tools.NewAuditLogger(aw), tools.NewHITLGate(threshold), sess), aw
}

func makeToolJob(tenantID, sessionID uuid.UUID, confidence float64, idemKey string) *river.Job[ExecuteToolArgs] {
	return &river.Job[ExecuteToolArgs]{
		Args: ExecuteToolArgs{
			TenantID:       tenantID,
			SessionID:      sessionID,
			ToolName:       "t",
			Args:           json.RawMessage(`{}`),
			IdempotencyKey: idemKey,
			Confidence:     confidence,
		},
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// WriteToolAboveThreshold: confidence ≥ threshold → execute + audit log written.
func TestExecuteToolWorker_writeTool_aboveThreshold_executesAndLogs(t *testing.T) {
	tenantID := uuid.New()
	sessID := uuid.New()
	sess := newMockSessionRepo(&Session{ID: sessID, TenantID: tenantID, State: StateActive, Version: 1})

	w, aw := newToolWorker(tools.KindWrite, 0.5, sess)
	job := makeToolJob(tenantID, sessID, 0.9, "key-w1")
	ctx := tenant.WithTenantID(context.Background(), tenantID.String())

	if err := w.Work(ctx, job); err != nil {
		t.Fatalf("Work: %v", err)
	}
	if len(aw.written) != 1 {
		t.Errorf("expected 1 audit record, got %d", len(aw.written))
	}
}

// WriteToolBelowThreshold: confidence < threshold → session → WaitingForHuman, no execution.
func TestExecuteToolWorker_writeTool_belowThreshold_transitionsToWaiting(t *testing.T) {
	tenantID := uuid.New()
	sessID := uuid.New()
	sess := newMockSessionRepo(&Session{ID: sessID, TenantID: tenantID, State: StateActive, Version: 1})

	w, aw := newToolWorker(tools.KindWrite, 0.8, sess)
	job := makeToolJob(tenantID, sessID, 0.3, "key-w2")
	ctx := tenant.WithTenantID(context.Background(), tenantID.String())

	if err := w.Work(ctx, job); err != nil {
		t.Fatalf("Work: %v", err)
	}
	if len(aw.written) != 0 {
		t.Errorf("expected 0 audit records (tool not executed), got %d", len(aw.written))
	}

	sess.mu.Lock()
	transitioned := append([]context.Context(nil), sess.transitionCtxs...)
	sess.mu.Unlock()
	if len(transitioned) == 0 {
		t.Error("expected session Transition to be called for HITL gate")
	}

	// Verify the session ended in WaitingForHuman.
	s, _ := sess.GetByID(ctx, sessID)
	if s.State != StateWaitingForHuman {
		t.Errorf("expected state WaitingForHuman, got %s", s.State)
	}
}

// WriteToolIdempotency: already-executed key → skip execution silently.
func TestExecuteToolWorker_writeTool_idempotencySkip(t *testing.T) {
	tenantID := uuid.New()
	sessID := uuid.New()
	sess := newMockSessionRepo(&Session{ID: sessID, TenantID: tenantID, State: StateActive, Version: 1})

	w, aw := newToolWorker(tools.KindWrite, 0.0, sess)
	job := makeToolJob(tenantID, sessID, 1.0, "key-w3")
	ctx := tenant.WithTenantID(context.Background(), tenantID.String())

	if err := w.Work(ctx, job); err != nil {
		t.Fatalf("first Work: %v", err)
	}
	firstCount := len(aw.written)

	// Second run — must be a no-op.
	if err := w.Work(ctx, job); err != nil {
		t.Fatalf("second Work: %v", err)
	}
	if len(aw.written) != firstCount {
		t.Errorf("idempotency broken: second call added audit records (%d→%d)", firstCount, len(aw.written))
	}
}

// ReadTool: executes directly, no audit log.
func TestExecuteToolWorker_readTool_executesWithoutAudit(t *testing.T) {
	tenantID := uuid.New()
	sessID := uuid.New()
	sess := newMockSessionRepo(&Session{ID: sessID, TenantID: tenantID, State: StateActive, Version: 1})

	w, aw := newToolWorker(tools.KindRead, 0.0, sess)
	job := makeToolJob(tenantID, sessID, 0.0, "key-r1")
	ctx := tenant.WithTenantID(context.Background(), tenantID.String())

	if err := w.Work(ctx, job); err != nil {
		t.Fatalf("Work: %v", err)
	}
	if len(aw.written) != 0 {
		t.Errorf("Read tool must not write audit records, got %d", len(aw.written))
	}
}

// UnknownTool: registry returns ErrNotFound.
func TestExecuteToolWorker_unknownTool_returnsNotFound(t *testing.T) {
	reg := tools.NewRegistry()
	aw := newMockAuditWriterTJ()
	sess := newMockSessionRepo()
	w := NewExecuteToolWorker(reg, tools.NewAuditLogger(aw), tools.NewHITLGate(0), sess)

	tenantID := uuid.New()
	ctx := tenant.WithTenantID(context.Background(), tenantID.String())
	job := &river.Job[ExecuteToolArgs]{
		Args: ExecuteToolArgs{
			TenantID: tenantID, SessionID: uuid.New(),
			ToolName: "no-such-tool", Args: json.RawMessage(`{}`),
		},
	}

	if !errors.Is(w.Work(ctx, job), tools.ErrNotFound) {
		t.Error("expected ErrNotFound for unknown tool")
	}
}
