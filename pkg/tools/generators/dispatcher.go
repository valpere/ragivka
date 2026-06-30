package generators

import "context"

// Dispatcher routes a GenerateArtifactArgs to the correct Generator (FR-19).
type Dispatcher struct {
	pdf   Generator
	excel Generator
}

func NewDispatcher(pdf, excel Generator) *Dispatcher {
	return &Dispatcher{pdf: pdf, excel: excel}
}

// Dispatch selects the generator for artifactType and calls Generate(ctx, data).
// Returns ErrUnsupportedType for unknown types.
func (d *Dispatcher) Dispatch(ctx context.Context, artifactType ArtifactType, data any) (string, error) {
	switch artifactType {
	case ArtifactPDF:
		return d.pdf.Generate(ctx, data)
	case ArtifactExcel:
		return d.excel.Generate(ctx, data)
	default:
		return "", ErrUnsupportedType
	}
}
