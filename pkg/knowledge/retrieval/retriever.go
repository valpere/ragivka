package retrieval

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/valpere/ragivka/pkg/obs"
	"github.com/valpere/ragivka/pkg/tenant"
)

// pgHybridRetriever implements Retriever using PostgreSQL pgvector + tsvector (FR-10).
type pgHybridRetriever struct {
	db       RowQuerier
	embedder Embedder
	reranker Reranker
}

// poolQuerier bridges *pgxpool.Pool to RowQuerier.
// pgxpool.Pool.Query returns pgx.Rows which satisfies QueryRows.
type poolQuerier struct{ pool *pgxpool.Pool }

func (p *poolQuerier) Query(ctx context.Context, sql string, args ...any) (QueryRows, error) {
	return p.pool.Query(ctx, sql, args...)
}

// NewRetriever returns a Retriever backed by PostgreSQL hybrid search.
func NewRetriever(pool *pgxpool.Pool, emb Embedder, rr Reranker) Retriever {
	return &pgHybridRetriever{db: &poolQuerier{pool}, embedder: emb, reranker: rr}
}

// NewRetrieverFromQuerier constructs a Retriever with a custom RowQuerier; used in tests.
func NewRetrieverFromQuerier(q RowQuerier, emb Embedder, rr Reranker) Retriever {
	return &pgHybridRetriever{db: q, embedder: emb, reranker: rr}
}

// hybridSQL scores each chunk by a blend of cosine similarity and ts_rank,
// returning 2×topK candidates for the reranker.
//
// NOTE: Phase 2 accepts a sequential scan. Phase 3 should replace ORDER BY with an
// IVFFlat ANN pre-filter (ORDER BY embedding <=> $vec LIMIT k) union ts_rank results.
const hybridSQL = `
SELECT
    c.id,
    c.document_id,
    c.content,
    COALESCE(1.0 - (c.embedding <=> $2::vector), 0.0) AS vec_score,
    COALESCE(ts_rank(c.tsv, plainto_tsquery('english', $3)), 0.0) AS text_score
FROM chunk c
WHERE c.tenant_id = $1
ORDER BY
    ($4::float8 * COALESCE(1.0 - (c.embedding <=> $2::vector), 0.0)) +
    ((1.0 - $4::float8) * COALESCE(ts_rank(c.tsv, plainto_tsquery('english', $3)), 0.0)) DESC
LIMIT $5`

// Retrieve embeds the query, runs hybrid SQL, re-ranks, and returns topK results (FR-10, FR-11).
func (r *pgHybridRetriever) Retrieve(ctx context.Context, query string, topK int, alpha float64) ([]RankedChunk, error) {
	raw, err := tenant.GetTenantID(ctx)
	if err != nil {
		return nil, err
	}
	tenantID, err := uuid.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("retriever: invalid tenant UUID: %w", err)
	}

	vecs, err := r.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("retriever: embed query: %w", err)
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		return nil, fmt.Errorf("retriever: embedder returned empty vector for query")
	}
	queryVec := vectorLiteral(vecs[0])

	candidateK := topK * 2
	if candidateK < 10 {
		candidateK = 10
	}

	rows, err := r.db.Query(ctx, hybridSQL, tenantID, queryVec, query, alpha, candidateK)
	if err != nil {
		return nil, fmt.Errorf("retriever: query: %w", err)
	}
	defer rows.Close()

	var candidates []RankedChunk
	for rows.Next() {
		var c RankedChunk
		if err := rows.Scan(&c.ChunkID, &c.DocumentID, &c.Content, &c.VecScore, &c.TextScore); err != nil {
			return nil, fmt.Errorf("retriever: scan: %w", err)
		}
		c.Score = alpha*c.VecScore + (1-alpha)*c.TextScore
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("retriever: rows: %w", err)
	}

	ranked := r.reranker.Rerank(query, candidates, topK)

	// Log Recall@K as the fraction of topK slots filled (NFR-14 offline evaluation hook).
	recallAtK := 0.0
	if topK > 0 {
		recallAtK = float64(len(ranked)) / float64(topK)
	}
	obs.LogRetrievalQuality(ctx, tenantID.String(), uuid.New(), topK, recallAtK)

	return ranked, nil
}

// vectorLiteral formats a float32 slice as a PostgreSQL vector literal for ::vector cast.
// Mirrors ingestion.VectorLiteral; duplicated here to avoid a cross-package dependency.
func vectorLiteral(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	var sb strings.Builder
	sb.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	sb.WriteByte(']')
	return sb.String()
}
