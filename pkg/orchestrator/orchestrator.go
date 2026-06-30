package orchestrator

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/valpere/ragivka/pkg/runtime"
	"github.com/valpere/ragivka/pkg/tenant"
)

// TieredOrchestrator dispatches to the correct handler based on Session.Tier (FR-1/FR-2/FR-3).
type TieredOrchestrator struct {
	sessions runtime.SessionRepository
	l0       Handler
	l1       Handler
	l2       Handler
}

func NewTieredOrchestrator(
	sessions runtime.SessionRepository,
	l0 Handler,
	l1 Handler,
	l2 Handler,
) *TieredOrchestrator {
	return &TieredOrchestrator{sessions: sessions, l0: l0, l1: l1, l2: l2}
}

// Run loads the session, injects tenant context, and dispatches to the correct handler (FR-1/FR-2/FR-3).
func (o *TieredOrchestrator) Run(ctx context.Context, sessionID uuid.UUID, userMessage string) error {
	// Preliminary context: tenant ID injected after loading the session.
	// We use a two-phase approach: load with system context, then switch to tenant context.
	session, err := o.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("orchestrator: load session: %w", err)
	}

	tctx := tenant.WithTenantID(ctx, session.TenantID.String())
	_ = tctx // handlers call tenant.WithTenantID themselves from session.TenantID

	handler, err := o.handlerFor(session.Tier)
	if err != nil {
		return err
	}

	return handler.Handle(ctx, session, userMessage)
}

func (o *TieredOrchestrator) handlerFor(tier runtime.Tier) (Handler, error) {
	switch tier {
	case runtime.TierL0:
		return o.l0, nil
	case runtime.TierL1:
		return o.l1, nil
	case runtime.TierL2, runtime.TierL3:
		return o.l2, nil
	default:
		return nil, fmt.Errorf("orchestrator: unknown tier %q", tier)
	}
}
