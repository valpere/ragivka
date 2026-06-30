package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/valpere/ragivka/pkg/tenant"
)

// memStorageClient is an in-memory StorageClient for unit tests.
type memStorageClient struct {
	mu      sync.RWMutex
	objects map[string][]byte
}

func newMemStorage() *memStorageClient { return &memStorageClient{objects: map[string][]byte{}} }

func (m *memStorageClient) PutObject(_ context.Context, key string, body io.Reader, _ int64) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.objects[key] = data
	m.mu.Unlock()
	return nil
}

func (m *memStorageClient) PresignURL(_ context.Context, key string, ttl time.Duration) (string, error) {
	m.mu.RLock()
	_, ok := m.objects[key]
	m.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("key not found: %s", key)
	}
	return fmt.Sprintf("https://example.com/%s?expires=%d", key, time.Now().Add(ttl).Unix()), nil
}

func (m *memStorageClient) DeleteObject(_ context.Context, key string) error {
	m.mu.Lock()
	delete(m.objects, key)
	m.mu.Unlock()
	return nil
}

func withTenant(tenantID uuid.UUID) context.Context {
	return tenant.WithTenantID(context.Background(), tenantID.String())
}

// TestStorageClient_interface verifies the interface is satisfied at compile time.
func TestStorageClient_interface(t *testing.T) {
	var _ StorageClient = newMemStorage()
}

func TestMemStorage_putAndPresign(t *testing.T) {
	st := newMemStorage()
	ctx := context.Background()
	key := "tenant-123/doc.pdf"

	if err := st.PutObject(ctx, key, bytes.NewReader([]byte("pdf content")), 11); err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	url, err := st.PresignURL(ctx, key, time.Hour)
	if err != nil {
		t.Fatalf("PresignURL: %v", err)
	}
	if url == "" {
		t.Error("expected non-empty presigned URL")
	}
}

func TestMemStorage_deleteRemovesObject(t *testing.T) {
	st := newMemStorage()
	ctx := context.Background()
	key := "doc/x.txt"
	_ = st.PutObject(ctx, key, bytes.NewReader([]byte("data")), 4)
	if err := st.DeleteObject(ctx, key); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	if _, err := st.PresignURL(ctx, key, time.Minute); err == nil {
		t.Error("expected error for deleted key, got nil")
	}
}

func TestArtifact_missingTenantInContext(t *testing.T) {
	// tenantUUIDFromCtx must return an error (not panic) on missing tenant.
	_, err := tenantUUIDFromCtx(context.Background())
	if err == nil {
		t.Fatal("expected error for missing tenant, got nil")
	}
	if !errors.Is(err, tenant.ErrNoTenant) {
		t.Errorf("expected ErrNoTenant, got: %v", err)
	}
}

func TestArtifact_tenantUUIDFromCtx_invalidUUID(t *testing.T) {
	ctx := tenant.WithTenantID(context.Background(), "not-a-uuid")
	_, err := tenantUUIDFromCtx(ctx)
	if err == nil {
		t.Fatal("expected error for invalid UUID, got nil")
	}
}

func TestArtifact_tenantUUIDFromCtx_valid(t *testing.T) {
	id := uuid.New()
	ctx := withTenant(id)
	got, err := tenantUUIDFromCtx(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != id {
		t.Errorf("got %v, want %v", got, id)
	}
}
