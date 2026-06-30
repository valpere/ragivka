package generators

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/valpere/ragivka/pkg/tenant"
	"github.com/xuri/excelize/v2"
)

// ExcelGenerator renders an ExcelData struct into an XLSX file and stores it (FR-19).
type ExcelGenerator struct {
	storage   StorageClient
	artifacts ArtifactRepository
	keyPrefix string
}

func NewExcelGenerator(storage StorageClient, artifacts ArtifactRepository, keyPrefix string) *ExcelGenerator {
	return &ExcelGenerator{storage: storage, artifacts: artifacts, keyPrefix: keyPrefix}
}

// Generate renders data into XLSX, uploads to S3, writes an ARTIFACT row.
// data must be of type ExcelData; any other type returns ErrUnsupportedType.
func (g *ExcelGenerator) Generate(ctx context.Context, data any) (string, error) {
	ed, ok := data.(ExcelData)
	if !ok {
		return "", ErrUnsupportedType
	}

	buf, err := renderExcel(ed)
	if err != nil {
		return "", fmt.Errorf("excel: render: %w", err)
	}

	tenantID := tenant.MustGetTenantID(ctx)
	key := fmt.Sprintf("%s/%s/%s.xlsx", g.keyPrefix, tenantID, uuid.New())

	stored, err := g.storage.Upload(ctx, key, buf, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	if err != nil {
		return "", fmt.Errorf("excel: upload: %w", err)
	}

	if err := g.artifacts.Create(ctx, ArtifactRecord{
		TenantID:  tenantID,
		Type:      ArtifactExcel,
		S3Key:     stored,
		SizeBytes: len(buf),
	}); err != nil {
		return "", fmt.Errorf("excel: artifact record: %w", err)
	}

	return stored, nil
}

func renderExcel(ed ExcelData) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close() //nolint:errcheck

	sheet := ed.SheetName
	if sheet == "" {
		sheet = "Sheet1"
	}
	idx, err := f.NewSheet(sheet)
	if err != nil {
		return nil, err
	}
	f.SetActiveSheet(idx)

	for col, header := range ed.Headers {
		cell, _ := excelize.CoordinatesToCellName(col+1, 1)
		if err := f.SetCellValue(sheet, cell, header); err != nil {
			return nil, err
		}
	}

	for rowIdx, row := range ed.Rows {
		for col, val := range row {
			cell, _ := excelize.CoordinatesToCellName(col+1, rowIdx+2)
			if err := f.SetCellValue(sheet, cell, val); err != nil {
				return nil, err
			}
		}
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

