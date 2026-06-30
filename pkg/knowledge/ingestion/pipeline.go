package ingestion

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/valpere/ragivka/pkg/knowledge"
)

// EmbedBatchSize is the maximum number of chunks sent to the embedder per call.
// Keeps individual requests small enough to avoid timeouts on large models.
const EmbedBatchSize = 16

// Pipeline orchestrates the full ingestion flow (FR-8, FR-9, NFR-18):
//
//	Connector → Parser → Chunker → PIIScrubber → Embedder → Indexer
//
// Each stage is injected so implementations can be swapped (NFR-8).
type Pipeline struct {
	connector Connector
	scrubber  PIIScrubber
	embedder  Embedder
	indexer   Indexer
	docs      knowledge.DocumentRepository
	cfg       ChunkConfig
}

// NewPipeline constructs a Pipeline with all required stages.
func NewPipeline(
	connector Connector,
	scrubber PIIScrubber,
	embedder Embedder,
	indexer Indexer,
	docs knowledge.DocumentRepository,
	cfg ChunkConfig,
) *Pipeline {
	return &Pipeline{
		connector: connector,
		scrubber:  scrubber,
		embedder:  embedder,
		indexer:   indexer,
		docs:      docs,
		cfg:       cfg,
	}
}

// Ingest runs the full ingestion pipeline for a single document.
// It updates the document status to Processing → Ready (or Failed on error).
// The document type determines which parser is used.
func (p *Pipeline) Ingest(ctx context.Context, docID uuid.UUID, s3Key, docType string) error {
	if err := p.docs.UpdateStatus(ctx, docID, knowledge.StatusProcessing, ""); err != nil {
		return fmt.Errorf("pipeline: mark processing: %w", err)
	}

	if err := p.ingest(ctx, docID, s3Key, docType); err != nil {
		_ = p.docs.UpdateStatus(ctx, docID, knowledge.StatusFailed, err.Error())
		return err
	}

	return p.docs.UpdateStatus(ctx, docID, knowledge.StatusReady, "")
}

func (p *Pipeline) ingest(ctx context.Context, docID uuid.UUID, s3Key, docType string) error {
	// Stage 1: Fetch raw bytes from S3.
	rc, err := p.connector.Connect(ctx, s3Key)
	if err != nil {
		return fmt.Errorf("connector: %w", err)
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("read document: %w", err)
	}

	// Stage 2: Parse to plain text.
	parser, err := ParserFor(docType)
	if err != nil {
		return err
	}
	text, err := parser.Parse(data)
	if err != nil {
		return fmt.Errorf("parse %q: %w", docType, err)
	}

	// Stage 3: Chunk.
	chunks := Chunk(text, p.cfg)
	if len(chunks) == 0 {
		return fmt.Errorf("document produced no chunks after parsing")
	}

	// Stage 4: PII scrubbing — applied to chunk content only (NFR-18).
	for i := range chunks {
		chunks[i].Content = p.scrubber.Scrub(chunks[i].Content)
	}

	// Stage 5: Embed in batches.
	for start := 0; start < len(chunks); start += EmbedBatchSize {
		end := start + EmbedBatchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		batch := chunks[start:end]
		texts := make([]string, len(batch))
		for i, c := range batch {
			texts[i] = c.Content
		}
		embeddings, err := p.embedder.Embed(ctx, texts)
		if err != nil {
			return fmt.Errorf("embed batch [%d:%d]: %w", start, end, err)
		}
		for i := range batch {
			chunks[start+i].Embedding = embeddings[i]
		}
	}

	// Stage 6: Index chunks into PostgreSQL (FR-9).
	return p.indexer.Index(ctx, docID, chunks)
}
