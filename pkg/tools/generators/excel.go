package generators

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/valpere/ragivka/pkg/tenant"
	"github.com/xuri/excelize/v2"
)

// ExcelGenerator renders an ExcelData struct into an XLSX file and uploads to S3 (FR-19).
// ARTIFACT row creation is the caller's responsibility (GenerateArtifactWorker).
type ExcelGenerator struct {
	storage   StorageClient
	keyPrefix string
}

func NewExcelGenerator(storage StorageClient, keyPrefix string) *ExcelGenerator {
	return &ExcelGenerator{storage: storage, keyPrefix: keyPrefix}
}

// Generate renders data into XLSX and uploads to S3.
// Returns the S3 key and byte count.
// data must be ExcelData; any other type returns ErrUnsupportedType.
// If ed.S3Key is set, it is used as-is (idempotent on River retry).
// If empty, a UUID-based key is generated (first-run case).
func (g *ExcelGenerator) Generate(ctx context.Context, data any) (string, int, error) {
	ed, ok := data.(ExcelData)
	if !ok {
		return "", 0, ErrUnsupportedType
	}

	buf, err := renderExcel(ed)
	if err != nil {
		return "", 0, fmt.Errorf("excel: render: %w", err)
	}

	key := ed.S3Key
	if key == "" {
		tenantID := tenant.MustGetTenantID(ctx)
		key = fmt.Sprintf("%s/%s/%s.xlsx", g.keyPrefix, tenantID, uuid.New())
	}

	stored, err := g.storage.Upload(ctx, key, buf,
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	if err != nil {
		return "", 0, fmt.Errorf("excel: upload: %w", err)
	}

	return stored, len(buf), nil
}

func renderExcel(ed ExcelData) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close() //nolint:errcheck

	sheet := ed.SheetName
	if sheet == "" {
		sheet = "Sheet1"
	}

	// excelize.NewFile() already creates "Sheet1". Rename it rather than creating
	// a duplicate, which would produce an unintended second sheet.
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return nil, err
	}

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
