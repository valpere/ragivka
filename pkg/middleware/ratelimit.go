package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/valpere/ragivka/pkg/tenant"
)

// RateLimiter enforces a per-tenant request budget (FR-24).
// limit is requests allowed in the current fixed window (per minute).
type RateLimiter interface {
	Allow(ctx context.Context, tenantID string, limit int) (bool, error)
}

// RedisRateLimiter implements a fixed-window counter: INCR + EXPIRE on a
// "ratelimit:{tenantID}:{minute}" key (FR-24 scope, as specified).
type RedisRateLimiter struct {
	client *redis.Client
}

// NewRedisRateLimiter constructs a RedisRateLimiter.
func NewRedisRateLimiter(client *redis.Client) *RedisRateLimiter {
	return &RedisRateLimiter{client: client}
}

// Allow increments the counter for the current minute window and reports
// whether the request is within limit.
func (r *RedisRateLimiter) Allow(ctx context.Context, tenantID string, limit int) (bool, error) {
	key := fmt.Sprintf("ratelimit:%s:%s", tenantID, time.Now().UTC().Format("200601021504"))

	count, err := r.client.Incr(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("ratelimit: incr: %w", err)
	}
	if count == 1 {
		// First hit in this window — set expiry so the key self-cleans.
		if err := r.client.Expire(ctx, key, time.Minute).Err(); err != nil {
			return false, fmt.Errorf("ratelimit: expire: %w", err)
		}
	}
	return count <= int64(limit), nil
}

// LimitFunc resolves the per-tenant requests-per-minute threshold (NFR-20 —
// thresholds are configurable per tenant). Return a default constant for a
// single global limit.
type LimitFunc func(tenantID string) int

// RateLimit returns middleware that enforces limiter against the tenant ID
// already present in context (set by JWTAuth or an equivalent upstream
// middleware). Requests over the limit receive 429 with the standardized
// error envelope (NFR-21).
func RateLimit(limiter RateLimiter, limitFor LimitFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenantID, err := tenant.GetTenantID(r.Context())
			if err != nil {
				WriteError(w, r, http.StatusUnauthorized, "unauthorized", "missing tenant context")
				return
			}

			allowed, err := limiter.Allow(r.Context(), tenantID, limitFor(tenantID))
			if err != nil {
				WriteError(w, r, http.StatusInternalServerError, "internal_error", "rate limit check failed")
				return
			}
			if !allowed {
				WriteError(w, r, http.StatusTooManyRequests, "rate_limited", "request rate limit exceeded")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
