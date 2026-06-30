package ingestion

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/valpere/ragivka/pkg/tenant"
)

// Ingester is the minimal interface the worker uses to run the pipeline.
// Extracted from *Pipeline to allow test doubles.
type Ingester interface {
	Ingest(ctx context.Context, docID uuid.UUID, s3Key, docType string) error
}

// IngestDocumentArgs is the River job payload for async document ingestion (FR-8, NFR-4).
// The job is idempotent: re-running after a partial failure re-processes from scratch,
// but the indexer uses ON CONFLICT DO NOTHING so duplicate chunks are safe.
type IngestDocumentArgs struct {
	TenantID   uuid.UUID `json:"tenant_id"`
	DocumentID uuid.UUID `json:"document_id"`
	S3Key      string    `json:"s3_key"`
	DocType    string    `json:"doc_type"` // "txt" | "html" | "pdf" | "docx"
}

func (IngestDocumentArgs) Kind() string { return "ingest_document" }

// IngestDocumentWorker processes a single document through the ingestion pipeline (FR-8).
type IngestDocumentWorker struct {
	river.WorkerDefaults[IngestDocumentArgs]
	pipeline Ingester
}

// NewIngestDocumentWorker constructs a worker wired to the given Pipeline.
func NewIngestDocumentWorker(p Ingester) *IngestDocumentWorker {
	return &IngestDocumentWorker{pipeline: p}
}

// Work runs the full ingestion pipeline for a single document (FR-8, FR-9, NFR-18).
// The context is enriched with the tenant ID from the job args before calling the pipeline.
func (w *IngestDocumentWorker) Work(ctx context.Context, job *river.Job[IngestDocumentArgs]) error {
	args := job.Args
	if args.TenantID == uuid.Nil {
		return fmt.Errorf("ingest_document: missing tenant_id in job args")
	}
	// Inject tenant into context so all downstream repository calls are tenant-scoped (NFR-16).
	tctx := tenant.WithTenantID(ctx, args.TenantID.String())

	if err := w.pipeline.Ingest(tctx, args.DocumentID, args.S3Key, args.DocType); err != nil {
		return fmt.Errorf("ingest_document %s: %w", args.DocumentID, err)
	}
	return nil
}

// NewIngestDocumentJob returns a river.InsertManyParams-compatible arg for enqueuing ingestion.
func NewIngestDocumentJob(tenantID, docID uuid.UUID, s3Key, docType string) IngestDocumentArgs {
	return IngestDocumentArgs{
		TenantID:   tenantID,
		DocumentID: docID,
		S3Key:      s3Key,
		DocType:    docType,
	}
}

