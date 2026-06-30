package generators

import "context"

// Dispatcher routes an ArtifactType to the correct Generator (FR-19).
type Dispatcher struct {
	pdf   Generator
	excel Generator
}

func NewDispatcher(pdf, excel Generator) *Dispatcher {
	return &Dispatcher{pdf: pdf, excel: excel}
}

// Dispatch selects the generator for artifactType and calls Generate(ctx, data).
// Returns the S3 key, byte count, and any error. Returns ErrUnsupportedType for unknown types.
func (d *Dispatcher) Dispatch(ctx context.Context, artifactType ArtifactType, data any) (string, int, error) {
	switch artifactType {
	case ArtifactPDF:
		return d.pdf.Generate(ctx, data)
	case ArtifactExcel:
		return d.excel.Generate(ctx, data)
	default:
		return "", 0, ErrUnsupportedType
	}
}
