package runtime

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/valpere/ragivka/pkg/tenant"
	"github.com/valpere/ragivka/pkg/tools"
)

// ExecuteToolArgs is the River job payload for async tool execution (FR-16/FR-17/FR-18).
// IdempotencyKey must be checked against AUDIT_LOG before Write execution (NFR-15).
type ExecuteToolArgs struct {
	TenantID       uuid.UUID       `json:"tenant_id"`
	SessionID      uuid.UUID       `json:"session_id"`
	ToolName       string          `json:"tool_name"`
	Args           json.RawMessage `json:"args"`
	IdempotencyKey string          `json:"idempotency_key"`
	Confidence     float64         `json:"confidence"` // AI confidence — evaluated by HITLGate for Write tools
}

func (ExecuteToolArgs) Kind() string { return "execute_tool" }

// ExecuteToolWorker runs a tool job: HITL gate → idempotency check → execute → audit log.
// NFR-7: the tool Execute call is outside any DB transaction (no pool held open during execution).
type ExecuteToolWorker struct {
	river.WorkerDefaults[ExecuteToolArgs]
	registry *tools.Registry
	auditLog *tools.AuditLogger
	hitlGate *tools.HITLGate
	sessions SessionRepository
}

func NewExecuteToolWorker(
	registry *tools.Registry,
	auditLog *tools.AuditLogger,
	hitlGate *tools.HITLGate,
	sessions SessionRepository,
) *ExecuteToolWorker {
	return &ExecuteToolWorker{
		registry: registry,
		auditLog: auditLog,
		hitlGate: hitlGate,
		sessions: sessions,
	}
}

func (w *ExecuteToolWorker) Work(ctx context.Context, job *river.Job[ExecuteToolArgs]) error {
	args := job.Args
	tctx := tenant.WithTenantID(ctx, args.TenantID.String())

	tool, err := w.registry.Get(args.ToolName)
	if err != nil {
		return err
	}

	if tool.Kind() == tools.KindWrite {
		// HITL gate: low confidence → transition session to WaitingForHuman and stop (FR-18).
		if err := w.hitlGate.Evaluate(args.Confidence); err != nil {
			if !errors.Is(err, tools.ErrHITLRequired) {
				return err
			}
			session, serr := w.sessions.GetByID(tctx, args.SessionID)
			if serr != nil {
				return serr
			}
			_, terr := w.sessions.Transition(tctx, args.SessionID, session.State, StateWaitingForHuman, session.Version)
			if terr != nil && !errors.Is(terr, ErrOptimisticLock) {
				return terr
			}
			return nil
		}

		// Idempotency check: skip if this key was already executed (NFR-15).
		executed, err := w.auditLog.IsExecuted(tctx, args.IdempotencyKey)
		if err != nil {
			return err
		}
		if executed {
			return nil
		}

		// Execute the tool outside any DB transaction (NFR-7).
		result, err := w.registry.Execute(tctx, args.ToolName, args.Args, tools.KindWrite)
		if err != nil {
			return err
		}

		// Write AUDIT_LOG after successful execution (NFR-15).
		return w.auditLog.Log(tctx,
			args.IdempotencyKey, args.ToolName,
			args.TenantID.String(), args.SessionID.String(),
			args.Args, result,
		)
	}

	// Read and Draft tools: execute directly, no audit log required (FR-16, FR-17).
	_, err = w.registry.Execute(tctx, args.ToolName, args.Args, tool.Kind())
	return err
}
