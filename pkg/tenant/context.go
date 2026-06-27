package tenant

import (
	"context"
	"errors"
)

type contextKey string

const tenantIDKey contextKey = "tenant_id"

// ErrNoTenant is returned when a tenant ID is expected but missing from context.
var ErrNoTenant = errors.New("tenant_id not found in context")

// WithTenantID returns a new context with the injected tenant ID.
// NFR-16: All database queries must be strictly tenant-scoped via this context.
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantIDKey, tenantID)
}

// GetTenantID extracts the tenant ID from the context.
func GetTenantID(ctx context.Context) (string, error) {
	val := ctx.Value(tenantIDKey)
	if tenantID, ok := val.(string); ok && tenantID != "" {
		return tenantID, nil
	}
	return "", ErrNoTenant
}

// MustGetTenantID extracts the tenant ID or panics.
// Useful for ensuring NFR-16 invariants inside tenant-scoped repositories.
func MustGetTenantID(ctx context.Context) string {
	tenantID, err := GetTenantID(ctx)
	if err != nil {
		panic("invariant violation: " + err.Error())
	}
	return tenantID
}
