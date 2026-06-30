package retrieval_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/valpere/ragivka/pkg/knowledge/retrieval"
	"github.com/valpere/ragivka/pkg/tenant"
)

// ---------------------------------------------------------------------------
// Doubles
// ---------------------------------------------------------------------------

type mockRow struct {
	chunkID uuid.UUID
	docID   uuid.UUID
	content string
	vec     float64
	text    float64
}

type mockQueryRows struct {
	rows []mockRow
	idx  int
	err  error
}

func (m *mockQueryRows) Next() bool { m.idx++; return m.idx <= len(m.rows) }
func (m *mockQueryRows) Close()     {}
func (m *mockQueryRows) Err() error { return m.err }

func (m *mockQueryRows) Scan(dest ...any) error {
	if m.idx-1 >= len(m.rows) {
		return errors.New("mockQueryRows: out of range")
	}
	row := m.rows[m.idx-1]
	*dest[0].(*uuid.UUID) = row.chunkID
	*dest[1].(*uuid.UUID) = row.docID
	*dest[2].(*string) = row.content
	*dest[3].(*float64) = row.vec
	*dest[4].(*float64) = row.text
	return nil
}

type mockQuerier struct {
	rows *mockQueryRows
	err  error
}

func (q *mockQuerier) Query(_ context.Context, _ string, _ ...any) (retrieval.QueryRows, error) {
	if q.err != nil {
		return nil, q.err
	}
	return q.rows, nil
}

type mockEmbedder struct {
	vec []float32
	err error
}

func (e *mockEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if e.err != nil {
		return nil, e.err
	}
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = e.vec
	}
	return out, nil
}

func ctxWithTenant() context.Context {
	return tenant.WithTenantID(context.Background(), uuid.New().String())
}

func newRetriever(rows []mockRow, queryErr error, embedVec []float32) retrieval.Retriever {
	return retrieval.NewRetrieverFromQuerier(
		&mockQuerier{rows: &mockQueryRows{rows: rows}, err: queryErr},
		&mockEmbedder{vec: embedVec},
		retrieval.NewDotProductReranker(),
	)
}

// ---------------------------------------------------------------------------
// Reranker
// ---------------------------------------------------------------------------

func TestDotProductReranker_sortsByVecScore(t *testing.T) {
	rr := retrieval.NewDotProductReranker()
	candidates := []retrieval.RankedChunk{
		{Content: "low", VecScore: 0.3},
		{Content: "high", VecScore: 0.9},
		{Content: "mid", VecScore: 0.6},
	}
	got := rr.Rerank("q", candidates, 3)
	if got[0].Content != "high" || got[1].Content != "mid" || got[2].Content != "low" {
		t.Errorf("wrong order: %v %v %v", got[0].Content, got[1].Content, got[2].Content)
	}
}

func TestDotProductReranker_topKCapsResults(t *testing.T) {
	rr := retrieval.NewDotProductReranker()
	candidates := make([]retrieval.RankedChunk, 10)
	for i := range candidates {
		candidates[i].VecScore = float64(i) / 10
	}
	if got := rr.Rerank("q", candidates, 3); len(got) != 3 {
		t.Errorf("want 3 results, got %d", len(got))
	}
}

func TestDotProductReranker_topKBeyondLen(t *testing.T) {
	rr := retrieval.NewDotProductReranker()
	candidates := []retrieval.RankedChunk{{VecScore: 0.5}}
	if got := rr.Rerank("q", candidates, 10); len(got) != 1 {
		t.Errorf("want 1 result, got %d", len(got))
	}
}

func TestDotProductReranker_emptyInput(t *testing.T) {
	rr := retrieval.NewDotProductReranker()
	if got := rr.Rerank("q", nil, 5); len(got) != 0 {
		t.Errorf("want 0 results for nil input, got %d", len(got))
	}
}

func TestDotProductReranker_topKZeroReturnsAll(t *testing.T) {
	rr := retrieval.NewDotProductReranker()
	candidates := []retrieval.RankedChunk{{VecScore: 0.9}, {VecScore: 0.1}}
	if got := rr.Rerank("q", candidates, 0); len(got) != 2 {
		t.Errorf("want 2 for topK=0, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// Retriever
// ---------------------------------------------------------------------------

func TestRetriever_returnsRankedChunks(t *testing.T) {
	rows := []mockRow{
		{uuid.New(), uuid.New(), "content A", 0.9, 0.5},
		{uuid.New(), uuid.New(), "content B", 0.5, 0.8},
		{uuid.New(), uuid.New(), "content C", 0.7, 0.3},
	}
	r := newRetriever(rows, nil, make([]float32, 1024))
	chunks, err := r.Retrieve(ctxWithTenant(), "test query", 2, 0.7)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("want 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Content != "content A" {
		t.Errorf("want 'content A' first (highest VecScore), got %q", chunks[0].Content)
	}
}

func TestRetriever_blendedScoreCalculated(t *testing.T) {
	rows := []mockRow{{uuid.New(), uuid.New(), "x", 0.8, 0.4}}
	r := newRetriever(rows, nil, make([]float32, 4))
	chunks, err := r.Retrieve(ctxWithTenant(), "q", 5, 0.6)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected 1 chunk")
	}
	want := 0.6*0.8 + 0.4*0.4
	if diff := chunks[0].Score - want; diff < -1e-9 || diff > 1e-9 {
		t.Errorf("Score = %.6f, want %.6f", chunks[0].Score, want)
	}
}

func TestRetriever_missingTenantReturnsError(t *testing.T) {
	r := newRetriever(nil, nil, make([]float32, 4))
	_, err := r.Retrieve(context.Background(), "q", 5, 0.7)
	if err == nil {
		t.Fatal("expected error for missing tenant")
	}
}

func TestRetriever_embedErrorPropagates(t *testing.T) {
	r := retrieval.NewRetrieverFromQuerier(
		&mockQuerier{rows: &mockQueryRows{}},
		&mockEmbedder{err: errors.New("embed down")},
		retrieval.NewDotProductReranker(),
	)
	_, err := r.Retrieve(ctxWithTenant(), "q", 5, 0.7)
	if err == nil {
		t.Fatal("expected embedder error to propagate")
	}
}

func TestRetriever_queryErrorPropagates(t *testing.T) {
	r := newRetriever(nil, errors.New("db error"), make([]float32, 4))
	_, err := r.Retrieve(ctxWithTenant(), "q", 5, 0.7)
	if err == nil {
		t.Fatal("expected DB error to propagate")
	}
}

func TestRetriever_emptyResultsReturnsNil(t *testing.T) {
	r := newRetriever(nil, nil, make([]float32, 4))
	chunks, err := r.Retrieve(ctxWithTenant(), "q", 5, 0.7)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if chunks != nil {
		t.Errorf("expected nil for empty result, got %v", chunks)
	}
}

func TestRetriever_alphaOneVectorFirst(t *testing.T) {
	rows := []mockRow{
		{uuid.New(), uuid.New(), "vec-high", 0.95, 0.1},
		{uuid.New(), uuid.New(), "text-high", 0.2, 0.99},
	}
	r := newRetriever(rows, nil, make([]float32, 4))
	chunks, err := r.Retrieve(ctxWithTenant(), "q", 2, 1.0)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(chunks) == 0 || chunks[0].Content != "vec-high" {
		t.Errorf("with alpha=1.0 expected 'vec-high' first, got %q", chunks[0].Content)
	}
}

func TestRetriever_candidateKMinimum(t *testing.T) {
	// topK=1 → candidateK should be 10 (minimum), so all 3 rows are fetched.
	rows := []mockRow{
		{uuid.New(), uuid.New(), "A", 0.9, 0.1},
		{uuid.New(), uuid.New(), "B", 0.8, 0.2},
		{uuid.New(), uuid.New(), "C", 0.7, 0.3},
	}
	r := newRetriever(rows, nil, make([]float32, 4))
	chunks, err := r.Retrieve(ctxWithTenant(), "q", 1, 0.5)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(chunks) != 1 {
		t.Errorf("want 1 after rerank topK=1, got %d", len(chunks))
	}
}

// ---------------------------------------------------------------------------
// vectorLiteral regression (via retriever round-trip: embed→format→query)
// ---------------------------------------------------------------------------

func TestRetriever_zeroVectorFormatted(t *testing.T) {
	// Ensures the zero vector (all 0.0) doesn't break the ::vector cast path.
	r := newRetriever(nil, nil, make([]float32, 4))
	// No rows → just checks no panic/error from embed+format path.
	_, err := r.Retrieve(ctxWithTenant(), "q", 5, 0.5)
	if err != nil {
		t.Fatalf("zero vector should not cause error: %v", err)
	}
}
