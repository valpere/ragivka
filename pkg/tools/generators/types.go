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

// Generator renders structured data into a binary artifact and stores it (FR-19).
// Implementations must never accept raw LLM text — callers pass typed structs.
// The returned s3Key is the storage path written to the ARTIFACT row.
type Generator interface {
	Generate(ctx context.Context, data any) (s3Key string, err error)
}

// StorageClient abstracts S3-compatible artifact storage (FR-19).
// The production implementation uses the AWS SDK; tests use an in-memory mock.
type StorageClient interface {
	// Upload stores bytes at the given key. Returns the final key on success.
	Upload(ctx context.Context, key string, data []byte, contentType string) (string, error)
}

// ArtifactRepository writes ARTIFACT rows after successful generation (FR-19).
// All writes must be tenant-scoped (NFR-16).
type ArtifactRepository interface {
	// Create records a generated artifact for a session.
	Create(ctx context.Context, rec ArtifactRecord) error
}

// ArtifactRecord is the ARTIFACT table row written after generation (FR-19).
type ArtifactRecord struct {
	TenantID  string
	SessionID string
	Type      ArtifactType
	S3Key     string
	SizeBytes int
}

// PDFData is the structured input for PDFGenerator (FR-19).
// The LLM provides this via ParseStructured[PDFData]; no raw text reaches the renderer.
type PDFData struct {
	Title    string
	Sections []PDFSection
}

// PDFSection is a titled block of text within a PDF report.
type PDFSection struct {
	Heading string
	Body    string
}

// ExcelData is the structured input for ExcelGenerator (FR-19).
// Headers and Rows must be provided by the LLM as a typed struct, not raw text.
type ExcelData struct {
	SheetName string
	Headers   []string
	Rows      [][]string
}
