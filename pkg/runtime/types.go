package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/valpere/ragivka/pkg/tenant"
)

// State represents the FSM state of a session (FR-5).
type State string

const (
	StateActive          State = "Active"
	StateWaitingForHuman State = "WaitingForHuman"
	StateCompleted       State = "Completed"
	StateExpired         State = "Expired"
)

// Tier is the orchestration tier selected for a session.
type Tier string

const (
	TierL0 Tier = "L0"
	TierL1 Tier = "L1"
	TierL2 Tier = "L2"
	TierL3 Tier = "L3"
)

// Session is the FSM container for a conversation (FR-5, FR-6, FR-7).
type Session struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	UserID    uuid.UUID
	State     State
	Version   int // optimistic locking counter (FR-6)
	Tier      Tier
	Channel   string
	ExpiresAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Message holds one turn in a conversation (FR-23).
type Message struct {
	ID           uuid.UUID
	SessionID    uuid.UUID
	TenantID     uuid.UUID
	Role         string // "user" | "assistant" | "system"
	Content      string
	CitationRefs []uuid.UUID // populated in Phase 2
	TokenCount   *int        // nil for user messages
	JobID        *int64      // non-nil for async (River) replies
	CreatedAt    time.Time
}

// SessionRepository is the data-access interface for sessions.
// All methods extract tenant_id from ctx via tenant.MustGetTenantID (NFR-16).
type SessionRepository interface {
	Create(ctx context.Context, s *Session) error
	GetByID(ctx context.Context, id uuid.UUID) (*Session, error)
	GetActiveByUserID(ctx context.Context, userID uuid.UUID) (*Session, error)
	// Transition atomically moves a session from→to and increments version.
	// Returns the new version on success, ErrOptimisticLock if version has changed.
	Transition(ctx context.Context, id uuid.UUID, from, to State, version int) (int, error)
	ListExpired(ctx context.Context) ([]*Session, error)
}

// MessageRepository is the data-access interface for messages.
type MessageRepository interface {
	Create(ctx context.Context, m *Message) error
	// ListForSession returns messages in chronological order, stopping when the
	// cumulative token budget is exceeded (FR-23 context window enforcement).
	ListForSession(ctx context.Context, sessionID uuid.UUID, maxTokens int) ([]*Message, error)
	GetByJobID(ctx context.Context, jobID int64) (*Message, error)
}

var (
	ErrNotFound       = errors.New("not found")
	ErrOptimisticLock = errors.New("optimistic lock conflict: session modified concurrently")
)

// tenantIDFromCtx extracts the tenant ID from ctx and parses it to uuid.UUID.
// The tenant middleware guarantees the value is a valid UUID, so parse errors
// indicate a misconfigured middleware and are treated as fatal programming errors.
func tenantIDFromCtx(ctx context.Context) (uuid.UUID, error) {
	raw := tenant.MustGetTenantID(ctx) // panics if missing — intentional
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("tenant ID in context is not a valid UUID: %w", err)
	}
	return id, nil
}
