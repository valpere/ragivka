package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/valpere/ragivka/pkg/middleware"
	"github.com/valpere/ragivka/pkg/runtime"
	"github.com/valpere/ragivka/pkg/tenant"
)

// DefaultSessionTTL is the inactivity window applied to sessions created via
// the web widget when no override is supplied (FR-7).
const DefaultSessionTTL = 30 * time.Minute

// CreateSessionRequest is the body for POST /v1/sessions.
type CreateSessionRequest struct {
	Tier runtime.Tier `json:"tier"`
}

// CreateSessionResponse is returned by POST /v1/sessions.
type CreateSessionResponse struct {
	SessionID string `json:"session_id"`
}

// NewCreateSessionHandler returns an http.Handler for POST /v1/sessions
// (FR-22). Tenant and user identity come from the JWT context injected by
// middleware.JWTAuth (NFR-23); a fresh session is created for that user.
func NewCreateSessionHandler(sessions runtime.SessionRepository, ttl time.Duration) http.Handler {
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			middleware.WriteError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}

		tenantID, err := tenant.GetTenantID(r.Context())
		if err != nil {
			middleware.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing tenant context")
			return
		}
		tenantUUID, err := uuid.Parse(tenantID)
		if err != nil {
			middleware.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "malformed tenant id")
			return
		}

		userID := middleware.UserIDFromContext(r.Context())
		userUUID, err := uuid.Parse(userID)
		if err != nil {
			middleware.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "malformed user id")
			return
		}

		var req CreateSessionRequest
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			// Body is optional; ignore decode errors from an empty body.
			_ = json.NewDecoder(r.Body).Decode(&req)
		}
		tier := req.Tier
		if tier == "" {
			tier = runtime.TierL0
		}

		sess := &runtime.Session{
			ID:        uuid.New(),
			TenantID:  tenantUUID,
			UserID:    userUUID,
			State:     runtime.StateActive,
			Version:   1,
			Tier:      tier,
			Channel:   "web",
			ExpiresAt: time.Now().Add(ttl),
		}
		if err := sessions.Create(r.Context(), sess); err != nil {
			middleware.WriteError(w, r, http.StatusInternalServerError, "internal_error", fmt.Sprintf("create session: %v", err))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(CreateSessionResponse{SessionID: sess.ID.String()})
	})
}
