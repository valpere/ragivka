package middleware

import (
	"encoding/json"
	"net/http"
)

// ErrorEnvelope is the standardized JSON error response shape (NFR-21).
type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody carries the machine-readable code, human message, and request ID.
type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

// WriteError writes a standardized error envelope (NFR-21) with the given
// HTTP status. The request ID is read from context (set by RequestID
// middleware); it is empty if that middleware was not applied.
func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorEnvelope{
		Error: ErrorBody{
			Code:      code,
			Message:   message,
			RequestID: RequestIDFromContext(r.Context()),
		},
	})
}
