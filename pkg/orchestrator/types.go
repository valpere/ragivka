package orchestrator

import (
	"context"

	"github.com/google/uuid"
	"github.com/valpere/ragivka/pkg/aicore"
	"github.com/valpere/ragivka/pkg/knowledge/retrieval"
	"github.com/valpere/ragivka/pkg/runtime"
)

// Orchestrator is the entry point for all pipeline tiers (FR-1, FR-2, FR-3).
// Implementations select the correct handler based on Session.Tier.
type Orchestrator interface {
	Run(ctx context.Context, sessionID uuid.UUID, userMessage string) error
}

// Handler executes one pipeline tier for a session (FR-1/FR-2/FR-3).
type Handler interface {
	Handle(ctx context.Context, session *runtime.Session, userMessage string) error
}

// JobEnqueuer abstracts River job insertion so L2Handler doesn't import *river.Client (testability).
type JobEnqueuer interface {
	EnqueueGenerateResponse(ctx context.Context, args runtime.GenerateResponseArgs) error
}

// deps groups the shared dependencies used across handlers.
// All handlers share the same sessions + messages repos; retriever/enqueuer are handler-specific.
type deps struct {
	router    aicore.ModelRouter
	sessions  runtime.SessionRepository
	messages  runtime.MessageRepository
	retriever retrieval.Retriever // nil for L0/L2
	enqueuer  JobEnqueuer         // nil for L0/L1
}
