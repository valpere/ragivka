package orchestrator

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// MessageRequest is the body for POST /v1/sessions/{id}/messages.
type MessageRequest struct {
	Message string `json:"message"`
}

// NewMessageHandler returns an http.Handler for POST /v1/sessions/{id}/messages.
// The orchestrator is called synchronously; L2 returns immediately (fire-and-forget internally).
func NewMessageHandler(orch Orchestrator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Extract session ID from path: /v1/sessions/{id}/messages
		sessionIDStr := extractSessionID(r.URL.Path)
		sessionID, err := uuid.Parse(sessionIDStr)
		if err != nil {
			http.Error(w, "invalid session id", http.StatusBadRequest)
			return
		}

		var req MessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		if err := orch.Run(r.Context(), sessionID, req.Message); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusAccepted)
	})
}

// extractSessionID parses the UUID segment from /v1/sessions/{id}/messages.
func extractSessionID(path string) string {
	// path: /v1/sessions/<uuid>/messages
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// expected: ["v1", "sessions", "<uuid>", "messages"]
	if len(parts) == 4 && parts[0] == "v1" && parts[1] == "sessions" && parts[3] == "messages" {
		return parts[2]
	}
	return ""
}
