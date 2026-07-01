package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/valpere/ragivka/pkg/middleware"
	"github.com/valpere/ragivka/pkg/tenant"
)

// ---------------------------------------------------------------------------
// ErrorEnvelope / WriteError
// ---------------------------------------------------------------------------

func TestWriteError_producesStandardEnvelope(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	middleware.WriteError(w, r, http.StatusBadRequest, "bad_input", "field X is required")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
	var env middleware.ErrorEnvelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if env.Error.Code != "bad_input" {
		t.Errorf("code: got %q, want bad_input", env.Error.Code)
	}
	if env.Error.Message != "field X is required" {
		t.Errorf("message: got %q, want %q", env.Error.Message, "field X is required")
	}
}

func TestWriteError_includesRequestIDFromContext(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set(middleware.RequestIDHeader, "req-123")
	w := httptest.NewRecorder()

	middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		middleware.WriteError(w, r, http.StatusInternalServerError, "internal_error", "boom")
	})).ServeHTTP(w, r)

	var env middleware.ErrorEnvelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if env.Error.RequestID != "req-123" {
		t.Errorf("request_id: got %q, want req-123", env.Error.RequestID)
	}
}

// ---------------------------------------------------------------------------
// RequestID
// ---------------------------------------------------------------------------

func TestRequestID_generatesWhenAbsent(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	var seen string
	middleware.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = middleware.RequestIDFromContext(r.Context())
	})).ServeHTTP(w, r)

	if seen == "" {
		t.Error("expected generated request ID, got empty string")
	}
	if w.Header().Get(middleware.RequestIDHeader) != seen {
		t.Error("response header must echo the same request ID injected into context")
	}
}

func TestRequestID_reusesClientSuppliedHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set(middleware.RequestIDHeader, "client-supplied-id")
	w := httptest.NewRecorder()

	var seen string
	middleware.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = middleware.RequestIDFromContext(r.Context())
	})).ServeHTTP(w, r)

	if seen != "client-supplied-id" {
		t.Errorf("request ID: got %q, want client-supplied-id", seen)
	}
}

// ---------------------------------------------------------------------------
// JWTAuth
// ---------------------------------------------------------------------------

func signToken(t *testing.T, secret []byte, claims middleware.Claims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString(secret)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return s
}

func TestJWTAuth_validToken_injectsTenantAndUser(t *testing.T) {
	secret := []byte("test-secret")
	tenantID := "11111111-1111-1111-1111-111111111111"
	claims := middleware.Claims{
		TenantID: tenantID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-42",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	tok := signToken(t, secret, claims)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()

	var gotTenant, gotUser string
	middleware.JWTAuth(secret)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotTenant, _ = tenant.GetTenantID(r.Context())
		gotUser = middleware.UserIDFromContext(r.Context())
	})).ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if gotTenant != tenantID {
		t.Errorf("tenant: got %q, want %q", gotTenant, tenantID)
	}
	if gotUser != "user-42" {
		t.Errorf("user: got %q, want user-42", gotUser)
	}
}

func TestJWTAuth_missingHeader_returns401(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	called := false
	middleware.JWTAuth([]byte("secret"))(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	})).ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
	if called {
		t.Error("downstream handler must not run for missing Authorization header")
	}
}

func TestJWTAuth_invalidSignature_returns401(t *testing.T) {
	claims := middleware.Claims{
		TenantID: "t1",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	tok := signToken(t, []byte("wrong-secret"), claims)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()

	middleware.JWTAuth([]byte("real-secret"))(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("downstream handler must not run for invalid signature")
	})).ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
}

func TestJWTAuth_expiredToken_returns401(t *testing.T) {
	secret := []byte("test-secret")
	claims := middleware.Claims{
		TenantID: "t1",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}
	tok := signToken(t, secret, claims)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()

	middleware.JWTAuth(secret)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("downstream handler must not run for expired token")
	})).ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
}

func TestJWTAuth_missingTenantClaim_returns401(t *testing.T) {
	secret := []byte("test-secret")
	claims := middleware.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	tok := signToken(t, secret, claims)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()

	middleware.JWTAuth(secret)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("downstream handler must not run when tenant_id claim is missing")
	})).ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
}

// ---------------------------------------------------------------------------
// RateLimit
// ---------------------------------------------------------------------------

type mockRateLimiter struct {
	allowed bool
	err     error
	calls   []string
}

func (m *mockRateLimiter) Allow(_ context.Context, tenantID string, _ int) (bool, error) {
	m.calls = append(m.calls, tenantID)
	return m.allowed, m.err
}

func TestRateLimit_allowed_callsNext(t *testing.T) {
	limiter := &mockRateLimiter{allowed: true}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(tenant.WithTenantID(r.Context(), "tenant-1"))
	w := httptest.NewRecorder()

	called := false
	middleware.RateLimit(limiter, func(string) int { return 60 })(
		http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { called = true }),
	).ServeHTTP(w, r)

	if !called {
		t.Error("expected downstream handler to run when allowed")
	}
	if len(limiter.calls) != 1 || limiter.calls[0] != "tenant-1" {
		t.Errorf("limiter called with wrong tenant: %v", limiter.calls)
	}
}

func TestRateLimit_exceeded_returns429(t *testing.T) {
	limiter := &mockRateLimiter{allowed: false}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(tenant.WithTenantID(r.Context(), "tenant-1"))
	w := httptest.NewRecorder()

	middleware.RateLimit(limiter, func(string) int { return 60 })(
		http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			t.Error("downstream handler must not run when rate limited")
		}),
	).ServeHTTP(w, r)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status: got %d, want 429", w.Code)
	}
}

func TestRateLimit_limiterError_returns500(t *testing.T) {
	limiter := &mockRateLimiter{err: errors.New("redis: connection refused")}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(tenant.WithTenantID(r.Context(), "tenant-1"))
	w := httptest.NewRecorder()

	middleware.RateLimit(limiter, func(string) int { return 60 })(
		http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			t.Error("downstream handler must not run when limiter errors")
		}),
	).ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", w.Code)
	}
}

func TestRateLimit_missingTenant_returns401(t *testing.T) {
	limiter := &mockRateLimiter{allowed: true}
	r := httptest.NewRequest(http.MethodGet, "/", nil) // no tenant in context
	w := httptest.NewRecorder()

	middleware.RateLimit(limiter, func(string) int { return 60 })(
		http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			t.Error("downstream handler must not run without tenant context")
		}),
	).ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
	if len(limiter.calls) != 0 {
		t.Error("limiter must not be called when tenant is missing")
	}
}

// ---------------------------------------------------------------------------
// TelegramSecretAuth
// ---------------------------------------------------------------------------

func TestTelegramSecretAuth_validSecret_callsNext(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set(middleware.TelegramSecretHeader, "s3cr3t")
	w := httptest.NewRecorder()

	called := false
	middleware.TelegramSecretAuth("s3cr3t")(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	})).ServeHTTP(w, r)

	if !called {
		t.Error("expected downstream handler to run for matching secret")
	}
}

func TestTelegramSecretAuth_mismatchedSecret_returns401(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set(middleware.TelegramSecretHeader, "wrong")
	w := httptest.NewRecorder()

	middleware.TelegramSecretAuth("s3cr3t")(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("downstream handler must not run for mismatched secret")
	})).ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
}

func TestTelegramSecretAuth_missingHeader_returns401(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	w := httptest.NewRecorder()

	middleware.TelegramSecretAuth("s3cr3t")(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("downstream handler must not run for missing header")
	})).ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
}

func TestTelegramSecretAuth_emptyConfiguredSecret_alwaysDenies(t *testing.T) {
	// Regression: an unconfigured (empty) secret must not be satisfied by a
	// request with no header — that would be an auth bypass, not a safe default.
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	w := httptest.NewRecorder()

	middleware.TelegramSecretAuth("")(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("downstream handler must not run when the configured secret is empty")
	})).ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
}
