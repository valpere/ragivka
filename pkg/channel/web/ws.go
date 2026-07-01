package web

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/valpere/ragivka/pkg/middleware"
	"github.com/valpere/ragivka/pkg/runtime"
	"github.com/valpere/ragivka/pkg/tenant"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Origin checking belongs to the deployment's reverse proxy / CORS
	// policy, not the widget handler itself.
	CheckOrigin: func(_ *http.Request) bool { return true },
}

// NewWebSocketHandler returns an http.Handler for GET /ws/sessions/{id}
// (FR-22). Upon a valid tenant-scoped session, the connection is subscribed
// to broadcaster and every published message is forwarded as a text frame
// until the client disconnects.
func NewWebSocketHandler(sessions runtime.SessionRepository, broadcaster Broadcaster) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionID, err := extractWSSessionID(r.URL.Path)
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
		if err != nil || sess.TenantID.String() != tenantID {
			middleware.WriteError(w, r, http.StatusNotFound, "not_found", "session not found")
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return // Upgrade already wrote the HTTP error response.
		}
		defer func() { _ = conn.Close() }()

		ch, cancel := broadcaster.Subscribe(r.Context(), sessionID)
		defer cancel()

		// Drain client-initiated frames (e.g. pings, close) in the
		// background so the connection is promptly detected as dead;
		// this handler does not accept client-sent chat messages.
		go func() {
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()

		for msg := range ch {
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	})
}

// extractWSSessionID parses the UUID segment from /ws/sessions/{id}.
func extractWSSessionID(path string) (uuid.UUID, error) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 3 || parts[0] != "ws" || parts[1] != "sessions" {
		return uuid.UUID{}, runtime.ErrNotFound
	}
	return uuid.Parse(parts[2])
}
