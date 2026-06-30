package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/valpere/ragivka/pkg/tenant"
	"github.com/valpere/ragivka/pkg/tools/generators"
)

// GenerateArtifactArgs is the River job payload for deterministic artifact generation (FR-19).
// DataJSON contains the LLM-parsed structured data; Type selects the generator.
type GenerateArtifactArgs struct {
	TenantID  uuid.UUID                  `json:"tenant_id"`
	SessionID uuid.UUID                  `json:"session_id"`
	Type      generators.ArtifactType    `json:"type"`
	DataJSON  json.RawMessage            `json:"data_json"` // typed struct serialised by ParseStructured[T]
}

func (GenerateArtifactArgs) Kind() string { return "generate_artifact" }

// GenerateArtifactWorker dispatches artifact generation jobs (FR-19).
// NFR-7: rendering and S3 upload happen outside any DB transaction.
type GenerateArtifactWorker struct {
	river.WorkerDefaults[GenerateArtifactArgs]
	dispatcher *generators.Dispatcher
}

func NewGenerateArtifactWorker(dispatcher *generators.Dispatcher) *GenerateArtifactWorker {
	return &GenerateArtifactWorker{dispatcher: dispatcher}
}

func (w *GenerateArtifactWorker) Work(ctx context.Context, job *river.Job[GenerateArtifactArgs]) error {
	args := job.Args
	tctx := tenant.WithTenantID(ctx, args.TenantID.String())

	data, err := unmarshalArtifactData(args.Type, args.DataJSON)
	if err != nil {
		return fmt.Errorf("generate_artifact: unmarshal data: %w", err)
	}

	if _, err := w.dispatcher.Dispatch(tctx, args.Type, data); err != nil {
		return fmt.Errorf("generate_artifact: dispatch: %w", err)
	}
	return nil
}

// unmarshalArtifactData deserialises DataJSON into the typed struct for the given ArtifactType.
// This is the boundary where JSON from ParseStructured[T] becomes a concrete Go value (FR-19).
func unmarshalArtifactData(t generators.ArtifactType, raw json.RawMessage) (any, error) {
	switch t {
	case generators.ArtifactPDF:
		var d generators.PDFData
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil, err
		}
		return d, nil
	case generators.ArtifactExcel:
		var d generators.ExcelData
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil, err
		}
		return d, nil
	default:
		return nil, generators.ErrUnsupportedType
	}
}
