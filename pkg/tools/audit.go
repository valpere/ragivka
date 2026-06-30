package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// AuditWriter is the persistence contract for AUDIT_LOG records (NFR-15).
// The production implementation uses pgxpool; tests use a mock.
type AuditWriter interface {
	IsExecuted(ctx context.Context, idempotencyKey string) (bool, error)
	Write(ctx context.Context, rec AuditRecord) error
}

// AuditLogger wraps AuditWriter with idempotency checking and hash computation (NFR-15).
// All Write Tool paths must call IsExecuted before executing and Log after (FR-18).
type AuditLogger struct {
	db AuditWriter
}

func NewAuditLogger(db AuditWriter) *AuditLogger {
	return &AuditLogger{db: db}
}

// IsExecuted returns true if the idempotency key is already recorded in AUDIT_LOG.
// Call this BEFORE executing a Write tool (NFR-15).
func (a *AuditLogger) IsExecuted(ctx context.Context, key string) (bool, error) {
	done, err := a.db.IsExecuted(ctx, key)
	if err != nil {
		return false, fmt.Errorf("audit: check idempotency: %w", err)
	}
	return done, nil
}

// Log writes an AUDIT_LOG record for a completed Write Tool execution (NFR-15).
// args and result are hashed with SHA-256; no raw data is stored.
func (a *AuditLogger) Log(ctx context.Context, key, toolName, tenantID, sessionID string, args, result json.RawMessage) error {
	if err := a.db.Write(ctx, AuditRecord{
		IdempotencyKey: key,
		ToolName:       toolName,
		TenantID:       tenantID,
		SessionID:      sessionID,
		RequestHash:    HashJSON(args),
		ResponseHash:   HashJSON(result),
	}); err != nil {
		return fmt.Errorf("audit: write: %w", err)
	}
	return nil
}
