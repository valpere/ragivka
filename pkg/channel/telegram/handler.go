package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valpere/ragivka/pkg/orchestrator"
	"github.com/valpere/ragivka/pkg/runtime"
	"github.com/valpere/ragivka/pkg/tenant"
)

// telegramUUIDNamespace derives a stable, deterministic UUID from a
// Telegram user ID so sessions can be resolved/created without a separate
// USER lookup table (out of scope for this adapter — see FR-21 scope).
var telegramUUIDNamespace = uuid.MustParse("6f6d0f2e-6f6c-4a7c-9f3e-3d6c1f2b9a11")

// DefaultSessionTTL is the inactivity window applied to sessions created via
// the Telegram adapter when no override is supplied (FR-7).
const DefaultSessionTTL = 30 * time.Minute

// NewWebhookHandler returns an http.Handler for POST /telegram/webhook/{tenantID}
// (FR-21). tenantID is taken from the path since each tenant is expected to
// register its own bot + webhook URL; the handler validates the request
// carries a valid tenant, resolves or creates a session keyed off the
// Telegram user ID, runs the orchestrator, and — for synchronous tiers
// (L0/L1) — sends the resulting assistant reply back to the chat.
// Async tiers (L2/L3) are acknowledged immediately; delivering their result
// back to Telegram requires a completion callback from the async worker,
// which is out of scope here (see pkg/orchestrator L2Handler doc comment).
func NewWebhookHandler(
	sessions runtime.SessionRepository,
	messages runtime.MessageRepository,
	orch orchestrator.Orchestrator,
	sender Sender,
	ttl time.Duration,
) http.Handler {
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		tenantID, err := extractTenantID(r.URL.Path)
		if err != nil {
			http.Error(w, "invalid tenant id", http.StatusBadRequest)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var update Update
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			http.Error(w, "invalid update payload", http.StatusBadRequest)
			return
		}

		// Telegram expects a fast 200 for any update we don't act on
		// (edited messages, non-text content, etc.) to avoid retries.
		if update.Message == nil || update.Message.From == nil || update.Message.Text == "" {
			w.WriteHeader(http.StatusOK)
			return
		}

		ctx := tenant.WithTenantID(r.Context(), tenantID.String())
		userID := uuid.NewSHA1(telegramUUIDNamespace, []byte(fmt.Sprintf("telegram:%d", update.Message.From.ID)))

		sess, err := resolveOrCreateSession(ctx, sessions, tenantID, userID, ttl)
		if err != nil {
			slog.Error("telegram: resolve session failed", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		if err := orch.Run(ctx, sess.ID, update.Message.Text); err != nil {
			slog.Error("telegram: orchestrator run failed", "error", err, "session_id", sess.ID)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// Re-fetch the session rather than trusting the pre-Run value of
		// sess.Tier: if a future FSM transition escalates the tier during
		// Run (e.g. L0 → L2 on low confidence), the cached value would be
		// stale and this check would attempt a synchronous reply for a
		// session that no longer has one ready.
		current, err := sessions.GetByID(ctx, sess.ID)
		if err != nil {
			slog.Error("telegram: reload session failed", "error", err, "session_id", sess.ID)
			w.WriteHeader(http.StatusOK)
			return
		}

		// Synchronous tiers have an assistant reply ready immediately after
		// Run returns; async tiers (L2/L3) do not — nothing to send yet.
		if current.Tier == runtime.TierL0 || current.Tier == runtime.TierL1 {
			if err := replyWithLatestAssistantMessage(ctx, messages, sess.ID, update.Message.Chat.ID, sender); err != nil {
				slog.Error("telegram: reply failed", "error", err, "session_id", sess.ID)
			}
		}

		w.WriteHeader(http.StatusOK)
	})
}

func resolveOrCreateSession(
	ctx context.Context,
	sessions runtime.SessionRepository,
	tenantID, userID uuid.UUID,
	ttl time.Duration,
) (*runtime.Session, error) {
	sess, err := sessions.GetActiveByUserID(ctx, userID)
	if err == nil {
		return sess, nil
	}
	if !errors.Is(err, runtime.ErrNotFound) {
		return nil, fmt.Errorf("get active session: %w", err)
	}

	sess = &runtime.Session{
		ID:        uuid.New(),
		TenantID:  tenantID,
		UserID:    userID,
		State:     runtime.StateActive,
		Version:   1,
		Tier:      runtime.TierL0,
		Channel:   "telegram",
		ExpiresAt: time.Now().Add(ttl),
	}
	if err := sessions.Create(ctx, sess); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return sess, nil
}

func replyWithLatestAssistantMessage(
	ctx context.Context,
	messages runtime.MessageRepository,
	sessionID uuid.UUID,
	chatID int64,
	sender Sender,
) error {
	history, err := messages.ListForSession(ctx, sessionID, 0)
	if err != nil {
		return fmt.Errorf("load history: %w", err)
	}
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "assistant" {
			return sender.SendMessage(ctx, chatID, history[i].Content)
		}
	}
	return errors.New("no assistant message found after orchestrator run")
}

// extractTenantID parses the UUID segment from /telegram/webhook/{tenantID}.
func extractTenantID(path string) (uuid.UUID, error) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 3 || parts[0] != "telegram" || parts[1] != "webhook" {
		return uuid.UUID{}, runtime.ErrNotFound
	}
	return uuid.Parse(parts[2])
}
