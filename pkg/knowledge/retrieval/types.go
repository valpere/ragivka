package retrieval

import (
	"context"

	"github.com/google/uuid"
)

// RankedChunk is a retrieved chunk with its hybrid relevance score (FR-10, FR-11, FR-12).
type RankedChunk struct {
	ChunkID    uuid.UUID
	DocumentID uuid.UUID
	Content    string
	VecScore   float64 // cosine similarity component, 0–1
	TextScore  float64 // ts_rank keyword component
	Score      float64 // blended final score: alpha*vec + (1-alpha)*text
}

// Retriever performs hybrid vector + keyword search and returns ranked results (FR-10).
type Retriever interface {
	Retrieve(ctx context.Context, query string, topK int, alpha float64) ([]RankedChunk, error)
}

// Reranker reorders a candidate set using a secondary scoring pass (FR-11).
// Phase 2: dot-product rescore on VecScore.
// Phase 3: replace with a full cross-encoder without changing the interface.
//
// Rerank MAY sort candidates in place; callers must not reuse the slice after this call.
type Reranker interface {
	Rerank(query string, candidates []RankedChunk, topK int) []RankedChunk
}

// Embedder converts text into dense float vectors for semantic search.
// Mirrors ingestion.Embedder; defined locally so retrieval has no import dependency on ingestion.
// Any value satisfying ingestion.Embedder also satisfies this interface.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// QueryRows is the minimal subset of pgx.Rows used by the retriever.
// Exported so callers can inject a mock in tests without depending on pgx directly.
type QueryRows interface {
	Next() bool
	Scan(dest ...any) error
	Close()
	Err() error
}

// RowQuerier abstracts pgxpool.Pool.Query for testability.
// Implement this interface to inject a test double instead of a real database.
type RowQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (QueryRows, error)
}
