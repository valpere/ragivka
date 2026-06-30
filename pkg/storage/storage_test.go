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

// memArtifactRepository is an in-memory ArtifactRepository for unit and higher-level tests.
type memArtifactRepository struct {
	mu        sync.RWMutex
	artifacts map[uuid.UUID]*Artifact
}

func newMemArtifactRepo() *memArtifactRepository {
	return &memArtifactRepository{artifacts: map[uuid.UUID]*Artifact{}}
}

func (m *memArtifactRepository) Create(_ context.Context, a *Artifact) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	cp := *a
	m.mu.Lock()
	m.artifacts[a.ID] = &cp
	m.mu.Unlock()
	return nil
}

func (m *memArtifactRepository) GetByID(_ context.Context, id uuid.UUID) (*Artifact, error) {
	m.mu.RLock()
	a, ok := m.artifacts[id]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	cp := *a
	return &cp, nil
}

func (m *memArtifactRepository) ListForSession(_ context.Context, sessionID uuid.UUID) ([]*Artifact, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Artifact
	for _, a := range m.artifacts {
		if a.SessionID == sessionID {
			cp := *a
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *memArtifactRepository) Delete(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.artifacts[id]; !ok {
		return ErrNotFound
	}
	delete(m.artifacts, id)
	return nil
}

// TestArtifactRepository_interface verifies compile-time interface satisfaction.
func TestArtifactRepository_interface(t *testing.T) {
	var _ ArtifactRepository = newMemArtifactRepo()
}

func TestMemArtifactRepo_createAndGet(t *testing.T) {
	repo := newMemArtifactRepo()
	ctx := context.Background()
	sessionID := uuid.New()

	a := &Artifact{SessionID: sessionID, Type: "pdf", S3Key: "t/doc.pdf", SizeBytes: 1024}
	if err := repo.Create(ctx, a); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if a.ID == uuid.Nil {
		t.Fatal("Create must assign a non-nil ID")
	}

	got, err := repo.GetByID(ctx, a.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.S3Key != "t/doc.pdf" || got.Type != "pdf" {
		t.Errorf("unexpected artifact: %+v", got)
	}
}

func TestMemArtifactRepo_listForSession(t *testing.T) {
	repo := newMemArtifactRepo()
	ctx := context.Background()
	sid := uuid.New()

	for i := range 3 {
		_ = repo.Create(ctx, &Artifact{SessionID: sid, Type: "summary", S3Key: fmt.Sprintf("k/%d", i)})
	}
	// artifact for a different session — must not appear
	_ = repo.Create(ctx, &Artifact{SessionID: uuid.New(), Type: "pdf", S3Key: "other"})

	list, err := repo.ListForSession(ctx, sid)
	if err != nil {
		t.Fatalf("ListForSession: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("expected 3, got %d", len(list))
	}
}

func TestMemArtifactRepo_deleteNotFound(t *testing.T) {
	repo := newMemArtifactRepo()
	err := repo.Delete(context.Background(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestMemArtifactRepo_deleteRemoves(t *testing.T) {
	repo := newMemArtifactRepo()
	ctx := context.Background()
	a := &Artifact{Type: "excel", S3Key: "k/x.xlsx"}
	_ = repo.Create(ctx, a)

	if err := repo.Delete(ctx, a.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.GetByID(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got: %v", err)
	}
}
