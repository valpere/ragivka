package generators

import (
	"bytes"
	"context"
	"fmt"

	"github.com/go-pdf/fpdf"
	"github.com/google/uuid"
	"github.com/valpere/ragivka/pkg/tenant"
)

// PDFGenerator renders a PDFData struct into a PDF and stores it (FR-19).
// The LLM-provided structured data arrives via ParseStructured[PDFData];
// this generator never touches raw LLM output.
type PDFGenerator struct {
	storage   StorageClient
	artifacts ArtifactRepository
	keyPrefix string // e.g. "artifacts/pdf"
}

func NewPDFGenerator(storage StorageClient, artifacts ArtifactRepository, keyPrefix string) *PDFGenerator {
	return &PDFGenerator{storage: storage, artifacts: artifacts, keyPrefix: keyPrefix}
}

// Generate renders data into a PDF, uploads to S3, writes an ARTIFACT row.
// data must be of type PDFData; any other type returns ErrUnsupportedType.
func (g *PDFGenerator) Generate(ctx context.Context, data any) (string, error) {
	pd, ok := data.(PDFData)
	if !ok {
		return "", ErrUnsupportedType
	}

	buf, err := renderPDF(pd)
	if err != nil {
		return "", fmt.Errorf("pdf: render: %w", err)
	}

	tenantID := tenant.MustGetTenantID(ctx)
	key := fmt.Sprintf("%s/%s/%s.pdf", g.keyPrefix, tenantID, uuid.New())

	stored, err := g.storage.Upload(ctx, key, buf, "application/pdf")
	if err != nil {
		return "", fmt.Errorf("pdf: upload: %w", err)
	}

	if err := g.artifacts.Create(ctx, ArtifactRecord{
		TenantID:  tenantID,
		Type:      ArtifactPDF,
		S3Key:     stored,
		SizeBytes: len(buf),
	}); err != nil {
		return "", fmt.Errorf("pdf: artifact record: %w", err)
	}

	return stored, nil
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
