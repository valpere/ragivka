package storage

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
)

// StorageClient is the provider-agnostic interface for object storage (FR-20).
// Implementations: S3Client (AWS S3 or MinIO-compatible).
type StorageClient interface {
	// PutObject uploads body to the given key. size must equal the byte count of body.
	PutObject(ctx context.Context, key string, body io.Reader, size int64) error
	// PresignURL returns a time-limited URL for downloading the given key.
	PresignURL(ctx context.Context, key string, ttl time.Duration) (string, error)
	// DeleteObject removes the object at key. No error if key does not exist.
	DeleteObject(ctx context.Context, key string) error
}

// Artifact is a record of an object stored in S3-compatible storage (FR-20).
type Artifact struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	SessionID uuid.UUID
	Type      string // "raw_document" | "pdf" | "excel" | "summary"
	S3Key     string
	SizeBytes int64
	CreatedAt time.Time
}

// ArtifactRepository is the data-access interface for artifact records.
type ArtifactRepository interface {
	Create(ctx context.Context, a *Artifact) error
	GetByID(ctx context.Context, id uuid.UUID) (*Artifact, error)
	ListForSession(ctx context.Context, sessionID uuid.UUID) ([]*Artifact, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

// ErrNotFound is returned when an artifact does not exist or belongs to a different tenant.
var ErrNotFound = errors.New("artifact not found")

// S3Config holds connection parameters for an S3-compatible endpoint (FR-20).
type S3Config struct {
	Bucket          string
	Region          string
	Endpoint        string // empty = AWS; non-empty = custom (MinIO)
	AccessKeyID     string
	SecretAccessKey string
	UsePathStyle    bool // required for MinIO; off for AWS virtual-hosted style
}
