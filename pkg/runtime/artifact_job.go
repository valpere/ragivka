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
	TenantID  uuid.UUID               `json:"tenant_id"`
	SessionID uuid.UUID               `json:"session_id"`
	Type      generators.ArtifactType `json:"type"`
	DataJSON  json.RawMessage         `json:"data_json"` // typed struct from ParseStructured[T]
}

func (GenerateArtifactArgs) Kind() string { return "generate_artifact" }

// GenerateArtifactWorker dispatches artifact generation jobs (FR-19).
// NFR-7: rendering and S3 upload happen outside any DB transaction.
// The worker owns ARTIFACT row creation so SessionID is always populated (NFR-16).
type GenerateArtifactWorker struct {
	river.WorkerDefaults[GenerateArtifactArgs]
	dispatcher *generators.Dispatcher
	artifacts  generators.ArtifactRepository
	keyPrefix  string // S3 prefix, e.g. "artifacts"
}

func NewGenerateArtifactWorker(
	dispatcher *generators.Dispatcher,
	artifacts generators.ArtifactRepository,
	keyPrefix string,
) *GenerateArtifactWorker {
	return &GenerateArtifactWorker{
		dispatcher: dispatcher,
		artifacts:  artifacts,
		keyPrefix:  keyPrefix,
	}
}

func (w *GenerateArtifactWorker) Work(ctx context.Context, job *river.Job[GenerateArtifactArgs]) error {
	args := job.Args
	tctx := tenant.WithTenantID(ctx, args.TenantID.String())

	// Unmarshal and inject the deterministic S3 key so River retries are idempotent:
	// same TenantID+SessionID+Type → same key → S3 overwrites on retry.
	s3Key := deterministicKey(w.keyPrefix, args.Type, args.TenantID, args.SessionID)
	data, err := unmarshalArtifactData(args.Type, args.DataJSON, s3Key)
	if err != nil {
		return fmt.Errorf("generate_artifact: unmarshal data: %w", err)
	}

	stored, sizeBytes, err := w.dispatcher.Dispatch(tctx, args.Type, data)
	if err != nil {
		return fmt.Errorf("generate_artifact: dispatch: %w", err)
	}

	// Create ARTIFACT row after successful upload (NFR-16: includes SessionID).
	return w.artifacts.Create(tctx, generators.ArtifactRecord{
		TenantID:  args.TenantID.String(),
		SessionID: args.SessionID.String(),
		Type:      args.Type,
		S3Key:     stored,
		SizeBytes: sizeBytes,
	})
}

// deterministicKey returns a stable S3 key for a given job identity.
// Using TenantID+SessionID+Type ensures the key is fixed across River retries.
func deterministicKey(prefix string, t generators.ArtifactType, tenantID, sessionID uuid.UUID) string {
	ext := "bin"
	switch t {
	case generators.ArtifactPDF:
		ext = "pdf"
	case generators.ArtifactExcel:
		ext = "xlsx"
	}
	return fmt.Sprintf("%s/%s/%s/%s.%s", prefix, t, tenantID, sessionID, ext)
}

// unmarshalArtifactData deserialises DataJSON into the typed struct and injects the pre-computed
// S3 key. Passing the key here keeps generators idempotent across River retries (FR-19).
// This is the JSON→Go boundary: only structured data from ParseStructured[T] is accepted.
func unmarshalArtifactData(t generators.ArtifactType, raw json.RawMessage, s3Key string) (any, error) {
	switch t {
	case generators.ArtifactPDF:
		var d generators.PDFData
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil, err
		}
		d.S3Key = s3Key
		return d, nil
	case generators.ArtifactExcel:
		var d generators.ExcelData
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil, err
		}
		d.S3Key = s3Key
		return d, nil
	default:
		return nil, generators.ErrUnsupportedType
	}
}
