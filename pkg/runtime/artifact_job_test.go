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

func newTestDispatcher(storage generators.StorageClient, arts generators.ArtifactRepository) *generators.Dispatcher {
	return generators.NewDispatcher(
		generators.NewPDFGenerator(storage, arts, "artifacts/pdf"),
		generators.NewExcelGenerator(storage, arts, "artifacts/excel"),
	)
}

// ---------------------------------------------------------------------------
// GenerateArtifactWorker tests
// ---------------------------------------------------------------------------

func TestGenerateArtifactWorker_PDF_createsArtifact(t *testing.T) {
	storage := newMemStorageAJ()
	arts := &memArtifactsAJ{}
	w := NewGenerateArtifactWorker(newTestDispatcher(storage, arts))

	tenantID := uuid.New()
	dataJSON, _ := json.Marshal(generators.PDFData{
		Title:    "Test",
		Sections: []generators.PDFSection{{Heading: "H", Body: "body"}},
	})
	job := &river.Job[GenerateArtifactArgs]{
		Args: GenerateArtifactArgs{
			TenantID:  tenantID,
			SessionID: uuid.New(),
			Type:      generators.ArtifactPDF,
			DataJSON:  dataJSON,
		},
	}

	ctx := tenant.WithTenantID(context.Background(), tenantID.String())
	if err := w.Work(ctx, job); err != nil {
		t.Fatalf("Work: %v", err)
	}
	if len(arts.records) != 1 {
		t.Errorf("expected 1 artifact record, got %d", len(arts.records))
	}
	if arts.records[0].Type != generators.ArtifactPDF {
		t.Errorf("artifact type: got %q, want pdf", arts.records[0].Type)
	}
}

func TestGenerateArtifactWorker_Excel_createsArtifact(t *testing.T) {
	storage := newMemStorageAJ()
	arts := &memArtifactsAJ{}
	w := NewGenerateArtifactWorker(newTestDispatcher(storage, arts))

	tenantID := uuid.New()
	dataJSON, _ := json.Marshal(generators.ExcelData{
		SheetName: "Report",
		Headers:   []string{"A", "B"},
		Rows:      [][]string{{"1", "2"}},
	})
	job := &river.Job[GenerateArtifactArgs]{
		Args: GenerateArtifactArgs{
			TenantID:  tenantID,
			SessionID: uuid.New(),
			Type:      generators.ArtifactExcel,
			DataJSON:  dataJSON,
		},
	}

	ctx := tenant.WithTenantID(context.Background(), tenantID.String())
	if err := w.Work(ctx, job); err != nil {
		t.Fatalf("Work: %v", err)
	}
	if len(arts.records) != 1 {
		t.Errorf("expected 1 artifact record, got %d", len(arts.records))
	}
	if arts.records[0].Type != generators.ArtifactExcel {
		t.Errorf("artifact type: got %q, want excel", arts.records[0].Type)
	}
}

func TestGenerateArtifactWorker_unknownType_returnsError(t *testing.T) {
	w := NewGenerateArtifactWorker(newTestDispatcher(newMemStorageAJ(), &memArtifactsAJ{}))

	tenantID := uuid.New()
	job := &river.Job[GenerateArtifactArgs]{
		Args: GenerateArtifactArgs{
			TenantID: tenantID,
			Type:     "csv",
			DataJSON: json.RawMessage(`{}`),
		},
	}

	ctx := tenant.WithTenantID(context.Background(), tenantID.String())
	err := w.Work(ctx, job)
	if err == nil {
		t.Fatal("expected error for unknown type, got nil")
	}
}

// REGRESSION: LLM raw text must not reach the renderer — DataJSON must be
// valid typed JSON or the worker returns an unmarshal error.
func TestGenerateArtifactWorker_invalidDataJSON_returnsError(t *testing.T) {
	w := NewGenerateArtifactWorker(newTestDispatcher(newMemStorageAJ(), &memArtifactsAJ{}))

	tenantID := uuid.New()
	job := &river.Job[GenerateArtifactArgs]{
		Args: GenerateArtifactArgs{
			TenantID: tenantID,
			Type:     generators.ArtifactPDF,
			DataJSON: json.RawMessage(`not-valid-json`),
		},
	}

	ctx := tenant.WithTenantID(context.Background(), tenantID.String())
	if err := w.Work(ctx, job); err == nil {
		t.Fatal("expected unmarshal error for invalid DataJSON, got nil")
	}
}

// Verify unmarshalArtifactData returns ErrUnsupportedType for unknown type.
func TestUnmarshalArtifactData_unknownType(t *testing.T) {
	_, err := unmarshalArtifactData("csv", json.RawMessage(`{}`))
	if !errors.Is(err, generators.ErrUnsupportedType) {
		t.Errorf("expected ErrUnsupportedType, got %v", err)
	}
}
