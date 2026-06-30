package generators

import (
	"context"
	"errors"
)

// ErrUnsupportedType is returned when an unknown artifact type is requested.
var ErrUnsupportedType = errors.New("generators: unsupported artifact type")

// ArtifactType identifies the output format.
type ArtifactType string

const (
	ArtifactPDF   ArtifactType = "pdf"
	ArtifactExcel ArtifactType = "excel"
)

// Generator renders structured data into a binary artifact and uploads it to S3 (FR-19).
// Implementations must never accept raw LLM text — callers pass typed structs.
// ARTIFACT row creation is the caller's responsibility (see GenerateArtifactWorker).
type Generator interface {
	Generate(ctx context.Context, data any) (s3Key string, sizeBytes int, err error)
}

// StorageClient abstracts S3-compatible artifact storage (FR-19).
// The production implementation uses the AWS SDK; tests use an in-memory mock.
type StorageClient interface {
	// Upload stores bytes at the given key. Returns the final key on success.
	Upload(ctx context.Context, key string, data []byte, contentType string) (string, error)
}

// ArtifactRepository writes ARTIFACT rows after successful upload (FR-19).
// Owned by GenerateArtifactWorker, not by individual generators (NFR-16: needs SessionID).
type ArtifactRepository interface {
	Create(ctx context.Context, rec ArtifactRecord) error
}

// ArtifactRecord is the ARTIFACT table row (FR-19).
type ArtifactRecord struct {
	TenantID  string
	SessionID string
	Type      ArtifactType
	S3Key     string
	SizeBytes int
}

// PDFData is the structured input for PDFGenerator (FR-19).
// The LLM provides Title and Sections via ParseStructured[PDFData]; no raw text reaches the renderer.
// S3Key is set by the caller (GenerateArtifactWorker) to a deterministic value — if empty, a UUID fallback is used.
type PDFData struct {
	Title    string
	Sections []PDFSection
	S3Key    string `json:"-"` // caller-supplied deterministic S3 key; not from LLM
}

// PDFSection is a titled block of text within a PDF report.
type PDFSection struct {
	Heading string
	Body    string
}

// ExcelData is the structured input for ExcelGenerator (FR-19).
// Headers and Rows come from the LLM via ParseStructured[ExcelData]; no raw text reaches the renderer.
// S3Key is set by the caller (GenerateArtifactWorker) to a deterministic value — if empty, a UUID fallback is used.
type ExcelData struct {
	SheetName string
	Headers   []string
	Rows      [][]string
	S3Key     string `json:"-"` // caller-supplied deterministic S3 key; not from LLM
}
