package knowledge

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// DocumentStatus tracks the ingestion lifecycle of a document.
type DocumentStatus string

const (
	StatusPending    DocumentStatus = "pending"
	StatusProcessing DocumentStatus = "processing"
	StatusReady      DocumentStatus = "ready"
	StatusFailed     DocumentStatus = "failed"
)

// Document is a tenant-scoped raw document record (FR-8, FR-20).
// The actual bytes live in S3; this table tracks metadata and ingestion state.
type Document struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Type      string // "txt" | "html" | "pdf" | "docx"
	Filename  string
	S3Key     string
	SizeBytes int64
	Status    DocumentStatus
	ErrorMsg  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Chunk is a single text segment produced by the ingestion pipeline (FR-9).
// Embedding holds the bge-m3:latest vector (dim 1024); TSV is computed by the DB.
type Chunk struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	DocumentID uuid.UUID
	Content    string
	TokenCount int
	ChunkIndex int
	Embedding  []float32 // dim 1024
}

// DocumentRepository manages document lifecycle (status updates, lookups).
type DocumentRepository interface {
	Create(ctx context.Context, d *Document) error
	GetByID(ctx context.Context, id uuid.UUID) (*Document, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status DocumentStatus, errMsg string) error
}

// ErrNotFound is returned when a document does not exist for the calling tenant.
var ErrNotFound = errors.New("document not found")
