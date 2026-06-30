package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/valpere/ragivka/pkg/tenant"
	"github.com/valpere/ragivka/pkg/tools/generators"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

type memStorageAJ struct {
	uploads map[string][]byte
}

func newMemStorageAJ() *memStorageAJ {
	return &memStorageAJ{uploads: make(map[string][]byte)}
}

func (m *memStorageAJ) Upload(_ context.Context, key string, data []byte, _ string) (string, error) {
	m.uploads[key] = data
	return key, nil
}

type memArtifactsAJ struct {
	records []generators.ArtifactRecord
}

func (m *memArtifactsAJ) Create(_ context.Context, rec generators.ArtifactRecord) error {
	m.records = append(m.records, rec)
	return nil
}

func newTestWorker(storage generators.StorageClient, arts generators.ArtifactRepository) *GenerateArtifactWorker {
	d := generators.NewDispatcher(
		generators.NewPDFGenerator(storage, "artifacts/pdf"),
		generators.NewExcelGenerator(storage, "artifacts/excel"),
	)
	return NewGenerateArtifactWorker(d, arts, "artifacts")
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestGenerateArtifactWorker_PDF_createsArtifactWithSessionID(t *testing.T) {
	storage := newMemStorageAJ()
	arts := &memArtifactsAJ{}
	w := newTestWorker(storage, arts)

	tenantID := uuid.New()
	sessID := uuid.New()
	dataJSON, _ := json.Marshal(generators.PDFData{
		Title:    "Test",
		Sections: []generators.PDFSection{{Heading: "H", Body: "body"}},
	})
	job := &river.Job[GenerateArtifactArgs]{
		Args: GenerateArtifactArgs{
			TenantID: tenantID, SessionID: sessID,
			Type: generators.ArtifactPDF, DataJSON: dataJSON,
		},
	}

	ctx := tenant.WithTenantID(context.Background(), tenantID.String())
	if err := w.Work(ctx, job); err != nil {
		t.Fatalf("Work: %v", err)
	}
	if len(arts.records) != 1 {
		t.Fatalf("expected 1 artifact record, got %d", len(arts.records))
	}
	rec := arts.records[0]
	if rec.Type != generators.ArtifactPDF {
		t.Errorf("artifact type: got %q, want pdf", rec.Type)
	}
	// SessionID must be populated (bug fix: generators didn't have access to job SessionID).
	if rec.SessionID != sessID.String() {
		t.Errorf("SessionID: got %q, want %q", rec.SessionID, sessID)
	}
	if rec.SizeBytes <= 0 {
		t.Error("SizeBytes must be > 0")
	}
}

func TestGenerateArtifactWorker_Excel_createsArtifactWithSessionID(t *testing.T) {
	storage := newMemStorageAJ()
	arts := &memArtifactsAJ{}
	w := newTestWorker(storage, arts)

	tenantID := uuid.New()
	sessID := uuid.New()
	dataJSON, _ := json.Marshal(generators.ExcelData{
		SheetName: "Report",
		Headers:   []string{"A", "B"},
		Rows:      [][]string{{"1", "2"}},
	})
	job := &river.Job[GenerateArtifactArgs]{
		Args: GenerateArtifactArgs{
			TenantID: tenantID, SessionID: sessID,
			Type: generators.ArtifactExcel, DataJSON: dataJSON,
		},
	}

	ctx := tenant.WithTenantID(context.Background(), tenantID.String())
	if err := w.Work(ctx, job); err != nil {
		t.Fatalf("Work: %v", err)
	}
	if len(arts.records) != 1 {
		t.Fatalf("expected 1 artifact record, got %d", len(arts.records))
	}
	if arts.records[0].SessionID != sessID.String() {
		t.Errorf("SessionID: got %q, want %q", arts.records[0].SessionID, sessID)
	}
}

// REGRESSION: River retry must upload to the same S3 key (not a new UUID each time).
func TestGenerateArtifactWorker_deterministicS3Key_idempotentOnRetry(t *testing.T) {
	storage := newMemStorageAJ()
	arts := &memArtifactsAJ{}
	w := newTestWorker(storage, arts)

	tenantID := uuid.New()
	sessID := uuid.New()
	dataJSON, _ := json.Marshal(generators.PDFData{Title: "R"})
	job := &river.Job[GenerateArtifactArgs]{
		Args: GenerateArtifactArgs{
			TenantID: tenantID, SessionID: sessID,
			Type: generators.ArtifactPDF, DataJSON: dataJSON,
		},
	}
	ctx := tenant.WithTenantID(context.Background(), tenantID.String())

	if err := w.Work(ctx, job); err != nil {
		t.Fatalf("first Work: %v", err)
	}
	key1 := arts.records[0].S3Key

	if err := w.Work(ctx, job); err != nil {
		t.Fatalf("second Work (retry): %v", err)
	}
	key2 := arts.records[1].S3Key

	if key1 != key2 {
		t.Errorf("S3 key differs on retry: %q vs %q — upload is not idempotent", key1, key2)
	}
}

func TestGenerateArtifactWorker_unknownType_returnsError(t *testing.T) {
	w := newTestWorker(newMemStorageAJ(), &memArtifactsAJ{})

	tenantID := uuid.New()
	job := &river.Job[GenerateArtifactArgs]{
		Args: GenerateArtifactArgs{
			TenantID: tenantID, Type: "csv",
			DataJSON: json.RawMessage(`{}`),
		},
	}
	ctx := tenant.WithTenantID(context.Background(), tenantID.String())
	if w.Work(ctx, job) == nil {
		t.Fatal("expected error for unknown type, got nil")
	}
}

// REGRESSION: LLM raw text must not reach the renderer — DataJSON must be
// valid typed JSON or the worker returns an unmarshal error.
func TestGenerateArtifactWorker_invalidDataJSON_returnsError(t *testing.T) {
	w := newTestWorker(newMemStorageAJ(), &memArtifactsAJ{})

	tenantID := uuid.New()
	job := &river.Job[GenerateArtifactArgs]{
		Args: GenerateArtifactArgs{
			TenantID: tenantID, Type: generators.ArtifactPDF,
			DataJSON: json.RawMessage(`not-valid-json`),
		},
	}
	ctx := tenant.WithTenantID(context.Background(), tenantID.String())
	if w.Work(ctx, job) == nil {
		t.Fatal("expected unmarshal error for invalid DataJSON, got nil")
	}
}

// Verify unmarshalArtifactData returns ErrUnsupportedType for unknown type.
func TestUnmarshalArtifactData_unknownType(t *testing.T) {
	_, err := unmarshalArtifactData("csv", json.RawMessage(`{}`), "key")
	if !errors.Is(err, generators.ErrUnsupportedType) {
		t.Errorf("expected ErrUnsupportedType, got %v", err)
	}
}
