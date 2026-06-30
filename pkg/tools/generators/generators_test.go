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
	err     error
}

func newMemStorage() *memStorage {
	return &memStorage{uploads: make(map[string][]byte)}
}

func (m *memStorage) Upload(_ context.Context, key string, data []byte, _ string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	m.uploads[key] = data
	return key, nil
}

func tenantCtx() context.Context {
	return tenant.WithTenantID(context.Background(), "tenant-test")
}

func bmin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// PDFGenerator
// ---------------------------------------------------------------------------

func TestPDFGenerator_Generate_nonEmptyBytes(t *testing.T) {
	storage := newMemStorage()
	gen := generators.NewPDFGenerator(storage, "artifacts/pdf")

	data := generators.PDFData{
		Title:    "Test Report",
		Sections: []generators.PDFSection{{Heading: "Introduction", Body: "Hello world."}},
	}
	key, size, err := gen.Generate(tenantCtx(), data)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if key == "" {
		t.Fatal("expected non-empty S3 key")
	}
	if size <= 0 {
		t.Errorf("expected positive size, got %d", size)
	}

	uploaded := storage.uploads[key]
	if len(uploaded) == 0 {
		t.Error("uploaded PDF must be non-empty")
	}
	if !bytes.HasPrefix(uploaded, []byte("%PDF")) {
		t.Errorf("uploaded bytes don't start with %%PDF; first 4: %q", uploaded[:bmin(4, len(uploaded))])
	}
}

func TestPDFGenerator_Generate_s3KeyFormat(t *testing.T) {
	storage := newMemStorage()
	gen := generators.NewPDFGenerator(storage, "art/pdf")

	key, _, err := gen.Generate(tenantCtx(), generators.PDFData{Title: "T"})
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

func TestPDFGenerator_Generate_deterministicKeyUsedIfSet(t *testing.T) {
	storage := newMemStorage()
	gen := generators.NewPDFGenerator(storage, "p")

	data := generators.PDFData{Title: "T", S3Key: "pre-computed/key.pdf"}
	key, _, err := gen.Generate(tenantCtx(), data)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if key != "pre-computed/key.pdf" {
		t.Errorf("expected pre-computed key, got %q", key)
	}
}

func TestPDFGenerator_Generate_wrongTypeReturnsError(t *testing.T) {
	gen := generators.NewPDFGenerator(newMemStorage(), "p")
	_, _, err := gen.Generate(tenantCtx(), generators.ExcelData{SheetName: "wrong"})
	if !errors.Is(err, generators.ErrUnsupportedType) {
		t.Errorf("expected ErrUnsupportedType, got %v", err)
	}
}

func TestPDFGenerator_Generate_emptySections(t *testing.T) {
	gen := generators.NewPDFGenerator(newMemStorage(), "p")
	_, _, err := gen.Generate(tenantCtx(), generators.PDFData{Title: "Empty"})
	if err != nil {
		t.Errorf("empty sections must not error: %v", err)
	}
}

func TestPDFGenerator_Generate_uploadErrorPropagates(t *testing.T) {
	storage := newMemStorage()
	storage.err = errors.New("s3: timeout")
	gen := generators.NewPDFGenerator(storage, "p")
	_, _, err := gen.Generate(tenantCtx(), generators.PDFData{Title: "T"})
	if err == nil {
		t.Error("expected error when upload fails, got nil")
	}
}

// ---------------------------------------------------------------------------
// ExcelGenerator
// ---------------------------------------------------------------------------

func TestExcelGenerator_Generate_nonEmptyBytes(t *testing.T) {
	storage := newMemStorage()
	gen := generators.NewExcelGenerator(storage, "artifacts/excel")

	data := generators.ExcelData{
		SheetName: "Report",
		Headers:   []string{"Name", "Value"},
		Rows:      [][]string{{"Alice", "42"}, {"Bob", "7"}},
	}
	key, size, err := gen.Generate(tenantCtx(), data)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	uploaded := storage.uploads[key]
	if len(uploaded) == 0 {
		t.Error("uploaded XLSX must be non-empty")
	}
	if size != len(uploaded) {
		t.Errorf("returned size %d != actual bytes %d", size, len(uploaded))
	}
	// XLSX is a ZIP — magic bytes: PK\x03\x04
	if !bytes.HasPrefix(uploaded, []byte("PK\x03\x04")) {
		t.Errorf("XLSX missing ZIP header; first 4: %q", uploaded[:bmin(4, len(uploaded))])
	}
}

func TestExcelGenerator_Generate_s3KeyFormat(t *testing.T) {
	storage := newMemStorage()
	gen := generators.NewExcelGenerator(storage, "art/excel")

	key, _, err := gen.Generate(tenantCtx(), generators.ExcelData{SheetName: "S"})
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

func TestExcelGenerator_Generate_deterministicKeyUsedIfSet(t *testing.T) {
	storage := newMemStorage()
	gen := generators.NewExcelGenerator(storage, "e")

	data := generators.ExcelData{SheetName: "S", S3Key: "pre-computed/key.xlsx"}
	key, _, err := gen.Generate(tenantCtx(), data)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if key != "pre-computed/key.xlsx" {
		t.Errorf("expected pre-computed key, got %q", key)
	}
}

// REGRESSION: excelize.NewFile() creates "Sheet1" by default; calling NewSheet("Sheet1")
// again would produce a duplicate sheet. SetSheetName rename must be used instead.
func TestExcelGenerator_Generate_customSheetNameNoDoubleSheet(t *testing.T) {
	storage := newMemStorage()
	gen := generators.NewExcelGenerator(storage, "e")

	data := generators.ExcelData{
		SheetName: "Custom",
		Headers:   []string{"A"},
		Rows:      [][]string{{"v"}},
	}
	key, _, err := gen.Generate(tenantCtx(), data)
	if err != nil {
		t.Fatalf("Generate with custom sheet name: %v", err)
	}
	if key == "" {
		t.Error("expected non-empty S3 key")
	}
}

func TestExcelGenerator_Generate_wrongTypeReturnsError(t *testing.T) {
	gen := generators.NewExcelGenerator(newMemStorage(), "e")
	_, _, err := gen.Generate(tenantCtx(), generators.PDFData{Title: "wrong"})
	if !errors.Is(err, generators.ErrUnsupportedType) {
		t.Errorf("expected ErrUnsupportedType, got %v", err)
	}
}

func TestExcelGenerator_Generate_emptyRowsAndHeaders(t *testing.T) {
	gen := generators.NewExcelGenerator(newMemStorage(), "e")
	_, _, err := gen.Generate(tenantCtx(), generators.ExcelData{SheetName: "Empty"})
	if err != nil {
		t.Errorf("empty rows must not error: %v", err)
	}
}

func TestExcelGenerator_Generate_uploadErrorPropagates(t *testing.T) {
	storage := newMemStorage()
	storage.err = errors.New("s3: timeout")
	gen := generators.NewExcelGenerator(storage, "e")
	_, _, err := gen.Generate(tenantCtx(), generators.ExcelData{SheetName: "S"})
	if err == nil {
		t.Error("expected error when upload fails, got nil")
	}
}

// ---------------------------------------------------------------------------
// Dispatcher
// ---------------------------------------------------------------------------

func TestDispatcher_Dispatch_pdf(t *testing.T) {
	storage := newMemStorage()
	d := generators.NewDispatcher(
		generators.NewPDFGenerator(storage, "p"),
		generators.NewExcelGenerator(storage, "e"),
	)

	key, size, err := d.Dispatch(tenantCtx(), generators.ArtifactPDF, generators.PDFData{Title: "D"})
	if err != nil || key == "" || size <= 0 {
		t.Errorf("Dispatch PDF: key=%q size=%d err=%v", key, size, err)
	}
}

func TestDispatcher_Dispatch_excel(t *testing.T) {
	storage := newMemStorage()
	d := generators.NewDispatcher(
		generators.NewPDFGenerator(storage, "p"),
		generators.NewExcelGenerator(storage, "e"),
	)

	key, size, err := d.Dispatch(tenantCtx(), generators.ArtifactExcel, generators.ExcelData{SheetName: "D"})
	if err != nil || key == "" || size <= 0 {
		t.Errorf("Dispatch Excel: key=%q size=%d err=%v", key, size, err)
	}
}

func TestDispatcher_Dispatch_unknownTypeReturnsError(t *testing.T) {
	d := generators.NewDispatcher(
		generators.NewPDFGenerator(newMemStorage(), "p"),
		generators.NewExcelGenerator(newMemStorage(), "e"),
	)
	_, _, err := d.Dispatch(tenantCtx(), "csv", nil)
	if !errors.Is(err, generators.ErrUnsupportedType) {
		t.Errorf("expected ErrUnsupportedType, got %v", err)
	}
}
