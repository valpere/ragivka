package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/valpere/ragivka/pkg/tenant"
)

// Claims is the expected JWT payload shape (NFR-23).
// TenantID drives NFR-16 tenant isolation for every downstream query.
type Claims struct {
	TenantID string `json:"tenant_id"`
	jwt.RegisteredClaims
}

type userIDContextKey struct{}

// ErrMissingTenantClaim is returned when a token verifies but carries no tenant_id.
var ErrMissingTenantClaim = errors.New("jwt: token missing tenant_id claim")

// JWTAuth returns middleware that validates a Bearer JWT signed with secret
// (HMAC), then injects the tenant ID (NFR-16) and user ID (claims.Subject)
// into the request context. Requests without a valid token receive a 401
// with the standardized error envelope (NFR-21).
func JWTAuth(secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, err := bearerToken(r)
			if err != nil {
				WriteError(w, r, http.StatusUnauthorized, "unauthorized", err.Error())
				return
			}

			claims := &Claims{}
			token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
				}
				return secret, nil
			})
			if err != nil || !token.Valid {
				WriteError(w, r, http.StatusUnauthorized, "unauthorized", "invalid or expired token")
				return
			}
			if claims.TenantID == "" {
				WriteError(w, r, http.StatusUnauthorized, "unauthorized", ErrMissingTenantClaim.Error())
				return
			}

			ctx := tenant.WithTenantID(r.Context(), claims.TenantID)
			ctx = context.WithValue(ctx, userIDContextKey{}, claims.Subject)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserIDFromContext extracts the JWT subject (user ID) injected by JWTAuth.
// Returns "" if the middleware was not applied.
func UserIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(userIDContextKey{}).(string)
	return id
}

// WithUserID injects userID into ctx using the same key JWTAuth uses.
// Exposed for other adapters (e.g. Telegram, which authenticates via a
// webhook secret rather than a JWT) and for tests.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDContextKey{}, userID)
}

// bearerToken extracts the token from the "Authorization: Bearer <token>" header.
func bearerToken(r *http.Request) (string, error) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", errors.New("missing or malformed Authorization header")
	}
	tok := strings.TrimPrefix(h, prefix)
	if tok == "" {
		return "", errors.New("empty bearer token")
	}
	return tok, nil
}
