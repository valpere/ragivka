package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// ToolKind classifies a tool by its permission level and side-effect profile (NFR-10).
type ToolKind int8

const (
	KindRead  ToolKind = iota // read-only, no external side effects, cacheable (FR-16)
	KindDraft                 // constructs a proposed action; no external mutation (FR-17)
	KindWrite                 // executes irreversible external mutations (FR-18)
)

var (
	ErrNotFound       = errors.New("tool: not found")
	ErrAlreadyExists  = errors.New("tool: already registered")
	ErrKindViolation  = errors.New("tool: kind violation — caller lacks permission to invoke this tool")
	ErrAlreadyExecuted = errors.New("tool: idempotency key already executed")
	ErrHITLRequired   = errors.New("tool: confidence below threshold — HITL approval required")
)

// Tool is the core abstraction for all agent-callable tools (FR-16, FR-17, FR-18).
type Tool interface {
	Name() string
	Kind() ToolKind
	Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error)
}

// AuditRecord captures everything required by NFR-15 for a Write Tool execution.
type AuditRecord struct {
	IdempotencyKey string
	ToolName       string
	TenantID       string
	SessionID      string
	RequestHash    string // hex-encoded SHA-256 of args
	ResponseHash   string // hex-encoded SHA-256 of result
}

// HashJSON returns the hex-encoded SHA-256 hash of a JSON payload (NFR-15).
func HashJSON(data json.RawMessage) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
