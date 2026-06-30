package generators_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/valpere/ragivka/pkg/tenant"
	"github.com/valpere/ragivka/pkg/tools/generators"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

type memStorage struct {
	uploads map[string][]byte
}

func newMemStorage() *memStorage {
	return &memStorage{uploads: make(map[string][]byte)}
}

func (m *memStorage) Upload(_ context.Context, key string, data []byte, _ string) (string, error) {
	m.uploads[key] = data
	return key, nil
}

type memArtifacts struct {
	records []generators.ArtifactRecord
}

func (m *memArtifacts) Create(_ context.Context, rec generators.ArtifactRecord) error {
	m.records = append(m.records, rec)
	return nil
}

func tenantCtx() context.Context {
	return tenant.WithTenantID(context.Background(), "tenant-test")
}

// ---------------------------------------------------------------------------
// PDFGenerator
// ---------------------------------------------------------------------------

func TestPDFGenerator_Generate_nonEmptyBytes(t *testing.T) {
	storage := newMemStorage()
	arts := &memArtifacts{}
	gen := generators.NewPDFGenerator(storage, arts, "artifacts/pdf")

	data := generators.PDFData{
		Title:    "Test Report",
		Sections: []generators.PDFSection{{Heading: "Introduction", Body: "Hello world."}},
	}
	key, err := gen.Generate(tenantCtx(), data)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if key == "" {
		t.Fatal("expected non-empty S3 key")
	}

	uploaded := storage.uploads[key]
	if len(uploaded) == 0 {
		t.Error("uploaded PDF must be non-empty")
	}
	// PDF magic bytes: %PDF
	if !bytes.HasPrefix(uploaded, []byte("%PDF")) {
		t.Errorf("uploaded bytes don't start with PDF header; first 4: %q", uploaded[:min(4, len(uploaded))])
	}
}

func TestPDFGenerator_Generate_s3KeyFormat(t *testing.T) {
	storage := newMemStorage()
	gen := generators.NewPDFGenerator(storage, &memArtifacts{}, "art/pdf")

	key, err := gen.Generate(tenantCtx(), generators.PDFData{Title: "T"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.HasPrefix(key, "art/pdf/tenant-test/") {
		t.Errorf("S3 key %q does not match expected prefix art/pdf/tenant-test/", key)
	}
	if !strings.HasSuffix(key, ".pdf") {
		t.Errorf("S3 key %q must end with .pdf", key)
	}
}

func TestPDFGenerator_Generate_artifactRowCreated(t *testing.T) {
	storage := newMemStorage()
	arts := &memArtifacts{}
	gen := generators.NewPDFGenerator(storage, arts, "artifacts/pdf")

	if _, err := gen.Generate(tenantCtx(), generators.PDFData{Title: "X"}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(arts.records) != 1 {
		t.Fatalf("expected 1 artifact record, got %d", len(arts.records))
	}
	rec := arts.records[0]
	if rec.Type != generators.ArtifactPDF {
		t.Errorf("record type: got %q, want %q", rec.Type, generators.ArtifactPDF)
	}
	if rec.SizeBytes <= 0 {
		t.Error("SizeBytes must be > 0")
	}
}

func TestPDFGenerator_Generate_wrongTypeReturnsError(t *testing.T) {
	gen := generators.NewPDFGenerator(newMemStorage(), &memArtifacts{}, "p")
	_, err := gen.Generate(tenantCtx(), generators.ExcelData{SheetName: "wrong"})
	if !errors.Is(err, generators.ErrUnsupportedType) {
		t.Errorf("expected ErrUnsupportedType, got %v", err)
	}
}

func TestPDFGenerator_Generate_emptySections(t *testing.T) {
	gen := generators.NewPDFGenerator(newMemStorage(), &memArtifacts{}, "p")
	_, err := gen.Generate(tenantCtx(), generators.PDFData{Title: "Empty"})
	if err != nil {
		t.Errorf("empty sections must not error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ExcelGenerator
// ---------------------------------------------------------------------------

func TestExcelGenerator_Generate_nonEmptyBytes(t *testing.T) {
	storage := newMemStorage()
	arts := &memArtifacts{}
	gen := generators.NewExcelGenerator(storage, arts, "artifacts/excel")

	data := generators.ExcelData{
		SheetName: "Report",
		Headers:   []string{"Name", "Value"},
		Rows:      [][]string{{"Alice", "42"}, {"Bob", "7"}},
	}
	key, err := gen.Generate(tenantCtx(), data)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	uploaded := storage.uploads[key]
	if len(uploaded) == 0 {
		t.Error("uploaded XLSX must be non-empty")
	}
	// XLSX is a ZIP — magic bytes: PK\x03\x04
	if !bytes.HasPrefix(uploaded, []byte("PK\x03\x04")) {
		t.Errorf("XLSX missing ZIP header; first 4: %q", uploaded[:min(4, len(uploaded))])
	}
}

func TestExcelGenerator_Generate_s3KeyFormat(t *testing.T) {
	storage := newMemStorage()
	gen := generators.NewExcelGenerator(storage, &memArtifacts{}, "art/excel")

	key, err := gen.Generate(tenantCtx(), generators.ExcelData{SheetName: "S"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.HasPrefix(key, "art/excel/tenant-test/") {
		t.Errorf("S3 key %q does not match expected prefix", key)
	}
	if !strings.HasSuffix(key, ".xlsx") {
		t.Errorf("S3 key %q must end with .xlsx", key)
	}
}

func TestExcelGenerator_Generate_artifactRowCreated(t *testing.T) {
	arts := &memArtifacts{}
	gen := generators.NewExcelGenerator(newMemStorage(), arts, "art/excel")

	if _, err := gen.Generate(tenantCtx(), generators.ExcelData{SheetName: "S"}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(arts.records) != 1 {
		t.Fatalf("expected 1 artifact record, got %d", len(arts.records))
	}
	if arts.records[0].Type != generators.ArtifactExcel {
		t.Errorf("record type: got %q, want %q", arts.records[0].Type, generators.ArtifactExcel)
	}
}

func TestExcelGenerator_Generate_wrongTypeReturnsError(t *testing.T) {
	gen := generators.NewExcelGenerator(newMemStorage(), &memArtifacts{}, "e")
	_, err := gen.Generate(tenantCtx(), generators.PDFData{Title: "wrong"})
	if !errors.Is(err, generators.ErrUnsupportedType) {
		t.Errorf("expected ErrUnsupportedType, got %v", err)
	}
}

func TestExcelGenerator_Generate_emptyRowsAndHeaders(t *testing.T) {
	gen := generators.NewExcelGenerator(newMemStorage(), &memArtifacts{}, "e")
	_, err := gen.Generate(tenantCtx(), generators.ExcelData{SheetName: "Empty"})
	if err != nil {
		t.Errorf("empty rows must not error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Dispatcher
// ---------------------------------------------------------------------------

func TestDispatcher_Dispatch_pdf(t *testing.T) {
	storage := newMemStorage()
	arts := &memArtifacts{}
	d := generators.NewDispatcher(
		generators.NewPDFGenerator(storage, arts, "p"),
		generators.NewExcelGenerator(storage, arts, "e"),
	)

	key, err := d.Dispatch(tenantCtx(), generators.ArtifactPDF, generators.PDFData{Title: "D"})
	if err != nil || key == "" {
		t.Errorf("Dispatch PDF: key=%q err=%v", key, err)
	}
}

func TestDispatcher_Dispatch_excel(t *testing.T) {
	storage := newMemStorage()
	arts := &memArtifacts{}
	d := generators.NewDispatcher(
		generators.NewPDFGenerator(storage, arts, "p"),
		generators.NewExcelGenerator(storage, arts, "e"),
	)

	key, err := d.Dispatch(tenantCtx(), generators.ArtifactExcel, generators.ExcelData{SheetName: "D"})
	if err != nil || key == "" {
		t.Errorf("Dispatch Excel: key=%q err=%v", key, err)
	}
}

func TestDispatcher_Dispatch_unknownTypeReturnsError(t *testing.T) {
	d := generators.NewDispatcher(
		generators.NewPDFGenerator(newMemStorage(), &memArtifacts{}, "p"),
		generators.NewExcelGenerator(newMemStorage(), &memArtifacts{}, "e"),
	)
	_, err := d.Dispatch(tenantCtx(), "csv", nil)
	if !errors.Is(err, generators.ErrUnsupportedType) {
		t.Errorf("expected ErrUnsupportedType, got %v", err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
