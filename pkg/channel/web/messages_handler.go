package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valpere/ragivka/pkg/middleware"
	"github.com/valpere/ragivka/pkg/runtime"
	"github.com/valpere/ragivka/pkg/tenant"
)

// MessageView is the wire representation of one conversation turn.
type MessageView struct {
	ID           string    `json:"id"`
	Role         string    `json:"role"`
	Content      string    `json:"content"`
	CitationRefs []string  `json:"citation_refs,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// NewListMessagesHandler returns an http.Handler for GET /v1/sessions/{id}/messages
// (FR-22). Verifies the session belongs to the requesting tenant before
// returning history — cross-tenant access is a hard NFR-16 violation even if
// the session ID is guessed correctly.
func NewListMessagesHandler(sessions runtime.SessionRepository, messages runtime.MessageRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			middleware.WriteError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}

		sessionID, err := extractSessionID(r.URL.Path)
		if err != nil {
			middleware.WriteError(w, r, http.StatusBadRequest, "invalid_request", "invalid session id")
			return
		}

		tenantID, err := tenant.GetTenantID(r.Context())
		if err != nil {
			middleware.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing tenant context")
			return
		}

		sess, err := sessions.GetByID(r.Context(), sessionID)
		if err != nil {
			middleware.WriteError(w, r, http.StatusNotFound, "not_found", "session not found")
			return
		}
		if sess.TenantID.String() != tenantID {
			// Deliberately identical to the not-found case: do not leak
			// existence of sessions belonging to other tenants (NFR-16).
			middleware.WriteError(w, r, http.StatusNotFound, "not_found", "session not found")
			return
		}

		history, err := messages.ListForSession(r.Context(), sessionID, 0)
		if err != nil {
			middleware.WriteError(w, r, http.StatusInternalServerError, "internal_error", "failed to load messages")
			return
		}

		views := make([]MessageView, len(history))
		for i, m := range history {
			refs := make([]string, len(m.CitationRefs))
			for j, c := range m.CitationRefs {
				refs[j] = c.String()
			}
			views[i] = MessageView{
				ID:           m.ID.String(),
				Role:         m.Role,
				Content:      m.Content,
				CitationRefs: refs,
				CreatedAt:    m.CreatedAt,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(views)
	})
}

// extractSessionID parses the UUID segment from /v1/sessions/{id}/messages.
func extractSessionID(path string) (uuid.UUID, error) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 4 || parts[0] != "v1" || parts[1] != "sessions" || parts[3] != "messages" {
		return uuid.UUID{}, runtime.ErrNotFound
	}
	return uuid.Parse(parts[2])
}
