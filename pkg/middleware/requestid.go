package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

type contextKey string

const requestIDKey contextKey = "request_id"

// RequestIDHeader is the HTTP header used to propagate/return the request ID.
const RequestIDHeader = "X-Request-ID"

// RequestID injects a request ID into the request context and response
// header. If the client supplied X-Request-ID, it is reused; otherwise a new
// UUID is generated (NFR-21 — every error envelope carries a request_id).
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if id == "" {
			id = uuid.New().String()
		}
		w.Header().Set(RequestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext extracts the request ID injected by RequestID.
// Returns "" if the middleware was not applied.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}
