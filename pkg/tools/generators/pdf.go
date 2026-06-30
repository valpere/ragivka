package generators

import (
	"bytes"
	"context"
	"fmt"

	"github.com/go-pdf/fpdf"
	"github.com/google/uuid"
	"github.com/valpere/ragivka/pkg/tenant"
)

// PDFGenerator renders a PDFData struct into a PDF and uploads to S3 (FR-19).
// ARTIFACT row creation is the caller's responsibility (GenerateArtifactWorker).
type PDFGenerator struct {
	storage   StorageClient
	keyPrefix string // e.g. "artifacts/pdf"
}

func NewPDFGenerator(storage StorageClient, keyPrefix string) *PDFGenerator {
	return &PDFGenerator{storage: storage, keyPrefix: keyPrefix}
}

// Generate renders data into a PDF and uploads to S3.
// Returns the S3 key and byte count.
// data must be PDFData; any other type returns ErrUnsupportedType.
// If pd.S3Key is set, it is used as-is (idempotent on River retry).
// If empty, a UUID-based key is generated (first-run case).
func (g *PDFGenerator) Generate(ctx context.Context, data any) (string, int, error) {
	pd, ok := data.(PDFData)
	if !ok {
		return "", 0, ErrUnsupportedType
	}

	buf, err := renderPDF(pd)
	if err != nil {
		return "", 0, fmt.Errorf("pdf: render: %w", err)
	}

	key := pd.S3Key
	if key == "" {
		tenantID := tenant.MustGetTenantID(ctx)
		key = fmt.Sprintf("%s/%s/%s.pdf", g.keyPrefix, tenantID, uuid.New())
	}

	stored, err := g.storage.Upload(ctx, key, buf, "application/pdf")
	if err != nil {
		return "", 0, fmt.Errorf("pdf: upload: %w", err)
	}

	return stored, len(buf), nil
}

func renderPDF(pd PDFData) ([]byte, error) {
	f := fpdf.New("P", "mm", "A4", "")
	f.AddPage()

	f.SetFont("Helvetica", "B", 16)
	f.CellFormat(0, 10, pd.Title, "", 1, "C", false, 0, "")
	f.Ln(4)

	for _, sec := range pd.Sections {
		f.SetFont("Helvetica", "B", 12)
		f.CellFormat(0, 8, sec.Heading, "", 1, "L", false, 0, "")
		f.SetFont("Helvetica", "", 11)
		f.MultiCell(0, 6, sec.Body, "", "L", false)
		f.Ln(4)
	}

	var buf bytes.Buffer
	if err := f.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
