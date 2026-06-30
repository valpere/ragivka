package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/valpere/ragivka/pkg/tools"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

type echoTool struct {
	name string
	kind tools.ToolKind
}

func (e *echoTool) Name() string { return e.name }
func (e *echoTool) Kind() tools.ToolKind { return e.kind }
func (e *echoTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	return args, nil
}

type mockAuditWriter struct {
	executed map[string]bool
	written  []tools.AuditRecord
	writeErr error
}

func newMockAuditWriter() *mockAuditWriter {
	return &mockAuditWriter{executed: make(map[string]bool)}
}

func (m *mockAuditWriter) IsExecuted(_ context.Context, key string) (bool, error) {
	return m.executed[key], nil
}

func (m *mockAuditWriter) Write(_ context.Context, rec tools.AuditRecord) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	m.executed[rec.IdempotencyKey] = true
	m.written = append(m.written, rec)
	return nil
}

// ---------------------------------------------------------------------------
// Registry — kind boundary enforcement (NFR-10)
// ---------------------------------------------------------------------------

func TestRegistry_Execute_readToolInReadContext(t *testing.T) {
	r := tools.NewRegistry()
	if err := r.Register(&echoTool{"lookup", tools.KindRead}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	args := json.RawMessage(`{"id":1}`)
	got, err := r.Execute(context.Background(), "lookup", args, tools.KindRead)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if string(got) != string(args) {
		t.Errorf("got %s, want %s", got, args)
	}
}

func TestRegistry_Execute_writeToolInReadContextReturnsKindViolation(t *testing.T) {
	r := tools.NewRegistry()
	if err := r.Register(&echoTool{"charge", tools.KindWrite}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	_, err := r.Execute(context.Background(), "charge", nil, tools.KindRead)
	if !errors.Is(err, tools.ErrKindViolation) {
		t.Errorf("expected ErrKindViolation, got %v", err)
	}
}

func TestRegistry_Execute_writeToolInWriteContextSucceeds(t *testing.T) {
	r := tools.NewRegistry()
	if err := r.Register(&echoTool{"send-email", tools.KindWrite}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	args := json.RawMessage(`"hello"`)
	got, err := r.Execute(context.Background(), "send-email", args, tools.KindWrite)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if string(got) != string(args) {
		t.Errorf("got %s, want %s", got, args)
	}
}

func TestRegistry_Register_duplicateReturnsError(t *testing.T) {
	r := tools.NewRegistry()
	t1 := &echoTool{"myTool", tools.KindRead}
	if err := r.Register(t1); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := r.Register(t1); !errors.Is(err, tools.ErrAlreadyExists) {
		t.Errorf("expected ErrAlreadyExists on duplicate, got %v", err)
	}
}

func TestRegistry_Execute_unknownToolReturnsNotFound(t *testing.T) {
	r := tools.NewRegistry()
	_, err := r.Execute(context.Background(), "missing", nil, tools.KindWrite)
	if !errors.Is(err, tools.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRegistry_Execute_draftToolInReadContextReturnsViolation(t *testing.T) {
	r := tools.NewRegistry()
	if err := r.Register(&echoTool{"draft-email", tools.KindDraft}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	_, err := r.Execute(context.Background(), "draft-email", nil, tools.KindRead)
	if !errors.Is(err, tools.ErrKindViolation) {
		t.Errorf("expected ErrKindViolation, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// AuditLogger — idempotency check + audit log entries (NFR-15)
// ---------------------------------------------------------------------------

func TestAuditLogger_Log_writesRecord(t *testing.T) {
	db := newMockAuditWriter()
	logger := tools.NewAuditLogger(db)

	args := json.RawMessage(`{"amount":100}`)
	result := json.RawMessage(`{"status":"ok"}`)
	if err := logger.Log(context.Background(), "key-1", "charge", "tenant-1", "session-1", args, result); err != nil {
		t.Fatalf("Log: %v", err)
	}

	if len(db.written) != 1 {
		t.Fatalf("want 1 audit record, got %d", len(db.written))
	}
	rec := db.written[0]
	if rec.IdempotencyKey != "key-1" {
		t.Errorf("IdempotencyKey: got %q, want %q", rec.IdempotencyKey, "key-1")
	}
	if rec.TenantID != "tenant-1" {
		t.Errorf("TenantID: got %q, want %q", rec.TenantID, "tenant-1")
	}
	if rec.RequestHash == "" || rec.ResponseHash == "" {
		t.Error("RequestHash/ResponseHash must be non-empty")
	}
}

func TestAuditLogger_IsExecuted_falseBeforeLog(t *testing.T) {
	db := newMockAuditWriter()
	logger := tools.NewAuditLogger(db)

	done, err := logger.IsExecuted(context.Background(), "key-new")
	if err != nil {
		t.Fatalf("IsExecuted: %v", err)
	}
	if done {
		t.Error("expected IsExecuted=false for unseen key")
	}
}

func TestAuditLogger_IsExecuted_trueAfterLog(t *testing.T) {
	db := newMockAuditWriter()
	logger := tools.NewAuditLogger(db)

	args := json.RawMessage(`{}`)
	if err := logger.Log(context.Background(), "key-2", "tool", "t", "s", args, args); err != nil {
		t.Fatalf("Log: %v", err)
	}

	done, err := logger.IsExecuted(context.Background(), "key-2")
	if err != nil {
		t.Fatalf("IsExecuted: %v", err)
	}
	if !done {
		t.Error("expected IsExecuted=true after Log")
	}
}

// REGRESSION: duplicate execution must be detectable before re-running the tool.
func TestAuditLogger_IdempotencySkip_regression(t *testing.T) {
	db := newMockAuditWriter()
	logger := tools.NewAuditLogger(db)

	args := json.RawMessage(`{"charge":50}`)
	if err := logger.Log(context.Background(), "idem-key", "charge", "t", "s", args, args); err != nil {
		t.Fatalf("first Log: %v", err)
	}
	callCount := len(db.written)

	done, err := logger.IsExecuted(context.Background(), "idem-key")
	if err != nil {
		t.Fatalf("IsExecuted: %v", err)
	}
	if !done {
		t.Fatal("idempotency check must detect existing execution")
	}
	// Second call to Log should still work (caller is responsible for skipping).
	// This test verifies that IsExecuted=true signals the caller to skip.
	if len(db.written) != callCount {
		t.Errorf("AuditWriter.Write called %d times (extra writes leaked)", len(db.written)-callCount)
	}
}

// ---------------------------------------------------------------------------
// HITLGate — confidence threshold logic (FR-18)
// ---------------------------------------------------------------------------

func TestHITLGate_aboveThresholdReturnsNil(t *testing.T) {
	g := tools.NewHITLGate(0.7)
	if err := g.Evaluate(0.8); err != nil {
		t.Errorf("expected nil for confidence=0.8 >= threshold=0.7, got %v", err)
	}
}

func TestHITLGate_atThresholdReturnsNil(t *testing.T) {
	g := tools.NewHITLGate(0.7)
	if err := g.Evaluate(0.7); err != nil {
		t.Errorf("expected nil for confidence=0.7 == threshold=0.7, got %v", err)
	}
}

func TestHITLGate_belowThresholdReturnsErrHITLRequired(t *testing.T) {
	g := tools.NewHITLGate(0.7)
	err := g.Evaluate(0.5)
	if !errors.Is(err, tools.ErrHITLRequired) {
		t.Errorf("expected ErrHITLRequired for confidence=0.5 < threshold=0.7, got %v", err)
	}
}

func TestHITLGate_zeroThresholdAlwaysPasses(t *testing.T) {
	g := tools.NewHITLGate(0.0)
	if err := g.Evaluate(0.0); err != nil {
		t.Errorf("expected nil for zero threshold, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// HashJSON
// ---------------------------------------------------------------------------

func TestHashJSON_deterministicAndNonEmpty(t *testing.T) {
	data := json.RawMessage(`{"key":"value"}`)
	h1 := tools.HashJSON(data)
	h2 := tools.HashJSON(data)
	if h1 != h2 {
		t.Errorf("HashJSON is not deterministic: %s != %s", h1, h2)
	}
	if h1 == "" {
		t.Error("HashJSON returned empty string")
	}
}

func TestHashJSON_differentInputsDifferentHashes(t *testing.T) {
	a := tools.HashJSON(json.RawMessage(`{"x":1}`))
	b := tools.HashJSON(json.RawMessage(`{"x":2}`))
	if a == b {
		t.Errorf("different inputs produced same hash: %s", a)
	}
}
