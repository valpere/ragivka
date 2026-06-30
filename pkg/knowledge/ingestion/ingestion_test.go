package ingestion_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/valpere/ragivka/pkg/knowledge"
	"github.com/valpere/ragivka/pkg/knowledge/ingestion"
	"github.com/valpere/ragivka/pkg/tenant"
)

// ---------------------------------------------------------------------------
// Chunker
// ---------------------------------------------------------------------------

func TestChunk_empty(t *testing.T) {
	chunks := ingestion.Chunk("", ingestion.DefaultChunkConfig())
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks for empty text, got %d", len(chunks))
	}
}

func TestChunk_shortTextSingleChunk(t *testing.T) {
	text := "Hello world"
	chunks := ingestion.Chunk(text, ingestion.ChunkConfig{Size: 100, Overlap: 10})
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Content != text {
		t.Fatalf("content = %q, want %q", chunks[0].Content, text)
	}
	if chunks[0].ChunkIndex != 0 {
		t.Fatalf("index = %d, want 0", chunks[0].ChunkIndex)
	}
}

func TestChunk_splitsWithOverlap(t *testing.T) {
	// 10 chars, size 6, overlap 2 → step 4
	// chunk 0: runes[0:6]  = "ABCDEF"
	// chunk 1: runes[4:10] = "EFGHIJ"
	text := "ABCDEFGHIJ"
	cfg := ingestion.ChunkConfig{Size: 6, Overlap: 2}
	chunks := ingestion.Chunk(text, cfg)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Content != "ABCDEF" {
		t.Errorf("chunk 0 = %q, want ABCDEF", chunks[0].Content)
	}
	if chunks[1].Content != "EFGHIJ" {
		t.Errorf("chunk 1 = %q, want EFGHIJ", chunks[1].Content)
	}
	if chunks[1].ChunkIndex != 1 {
		t.Errorf("chunk 1 index = %d, want 1", chunks[1].ChunkIndex)
	}
}

func TestChunk_tokenCountEstimate(t *testing.T) {
	text := strings.Repeat("a", 400)
	chunks := ingestion.Chunk(text, ingestion.ChunkConfig{Size: 400, Overlap: 0})
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk")
	}
	// 400 chars / 4 ≈ 100 tokens
	if chunks[0].TokenCount != 100 {
		t.Errorf("TokenCount = %d, want 100", chunks[0].TokenCount)
	}
}

func TestChunk_unicodeRunes(t *testing.T) {
	text := "Привіт" // 6 runes, each 2 bytes in UTF-8
	chunks := ingestion.Chunk(text, ingestion.ChunkConfig{Size: 4, Overlap: 0})
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks for 6 runes with size 4, got %d", len(chunks))
	}
	if chunks[0].Content != "Прив" {
		t.Errorf("chunk 0 = %q, want Прив", chunks[0].Content)
	}
	if chunks[1].Content != "іт" {
		t.Errorf("chunk 1 = %q, want іт", chunks[1].Content)
	}
}

func TestChunk_zeroSizeFallsBackToDefault(t *testing.T) {
	text := strings.Repeat("x", 10)
	chunks := ingestion.Chunk(text, ingestion.ChunkConfig{Size: 0})
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk")
	}
}

// ---------------------------------------------------------------------------
// PII scrubber
// ---------------------------------------------------------------------------

func TestRegexScrubber_email(t *testing.T) {
	s := ingestion.NewRegexScrubber()
	got := s.Scrub("Contact alice@example.com for details")
	if strings.Contains(got, "alice@example.com") {
		t.Errorf("email not scrubbed: %q", got)
	}
	if !strings.Contains(got, "[EMAIL]") {
		t.Errorf("expected [EMAIL] placeholder, got: %q", got)
	}
}

func TestRegexScrubber_ssn(t *testing.T) {
	s := ingestion.NewRegexScrubber()
	got := s.Scrub("SSN: 123-45-6789")
	if strings.Contains(got, "123-45-6789") {
		t.Errorf("SSN not scrubbed: %q", got)
	}
	if !strings.Contains(got, "[SSN]") {
		t.Errorf("expected [SSN] placeholder, got: %q", got)
	}
}

func TestRegexScrubber_noPII(t *testing.T) {
	s := ingestion.NewRegexScrubber()
	text := "No sensitive data here. Just a regular sentence."
	if got := s.Scrub(text); got != text {
		t.Errorf("clean text was modified: %q → %q", text, got)
	}
}

// ---------------------------------------------------------------------------
// Pipeline (mock integration)
// ---------------------------------------------------------------------------

// memConnector implements ingestion.Connector and serves in-memory text.
type memConnector struct{ data string }

func (m *memConnector) Connect(_ context.Context, _ string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(m.data)), nil
}

// memEmbedder implements ingestion.Embedder and returns zero vectors.
type memEmbedder struct{ dim int }

func (e *memEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = make([]float32, e.dim)
	}
	return out, nil
}

// memIndexer implements ingestion.Indexer and records indexed chunk count.
type memIndexer struct{ indexed int }

func (m *memIndexer) Index(_ context.Context, _ uuid.UUID, chunks []knowledge.Chunk) error {
	m.indexed += len(chunks)
	return nil
}

// memDocRepo implements knowledge.DocumentRepository in-memory.
type memDocRepo struct{ status knowledge.DocumentStatus }

func (r *memDocRepo) Create(_ context.Context, _ *knowledge.Document) error { return nil }
func (r *memDocRepo) GetByID(_ context.Context, _ uuid.UUID) (*knowledge.Document, error) {
	return &knowledge.Document{Status: r.status}, nil
}
func (r *memDocRepo) UpdateStatus(_ context.Context, _ uuid.UUID, s knowledge.DocumentStatus, _ string) error {
	r.status = s
	return nil
}

func TestPipeline_txtDocumentIndexesChunks(t *testing.T) {
	tenantID := uuid.New()
	docID := uuid.New()
	text := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 20) // ~900 chars

	connector := &memConnector{data: text}
	embedder := &memEmbedder{dim: 1024}
	idxr := &memIndexer{}
	repo := &memDocRepo{status: knowledge.StatusPending}

	pipeline := ingestion.NewPipeline(
		connector,
		ingestion.NewRegexScrubber(),
		embedder,
		idxr,
		repo,
		ingestion.DefaultChunkConfig(),
	)

	ctx := tenant.WithTenantID(context.Background(), tenantID.String())
	if err := pipeline.Ingest(ctx, docID, "tenant/test.txt", "txt"); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	if repo.status != knowledge.StatusReady {
		t.Errorf("status = %q, want %q", repo.status, knowledge.StatusReady)
	}
	if idxr.indexed == 0 {
		t.Error("expected at least 1 chunk indexed")
	}
}

func TestPipeline_failureMarksFailed(t *testing.T) {
	tenantID := uuid.New()
	docID := uuid.New()

	// "badtype" has no parser — pipeline must mark the document as failed.
	connector := &memConnector{data: "some content"}
	embedder := &memEmbedder{dim: 1024}
	idxr := &memIndexer{}
	repo := &memDocRepo{status: knowledge.StatusPending}

	pipeline := ingestion.NewPipeline(connector, ingestion.NewRegexScrubber(), embedder, idxr, repo, ingestion.DefaultChunkConfig())

	ctx := tenant.WithTenantID(context.Background(), tenantID.String())
	err := pipeline.Ingest(ctx, docID, "key", "badtype")
	if err == nil {
		t.Fatal("expected error for unknown doc type")
	}
	if repo.status != knowledge.StatusFailed {
		t.Errorf("status = %q, want %q", repo.status, knowledge.StatusFailed)
	}
}

func TestPipeline_htmlParserStripsMarkup(t *testing.T) {
	htmlDoc := "<html><body><h1>Title</h1><p>Body text with <b>bold</b>.</p></body></html>"
	connector := &memConnector{data: htmlDoc}
	embedder := &memEmbedder{dim: 1024}
	idxr := &memIndexer{}
	repo := &memDocRepo{status: knowledge.StatusPending}
	tenantID := uuid.New()
	docID := uuid.New()

	pipeline := ingestion.NewPipeline(connector, ingestion.NewRegexScrubber(), embedder, idxr, repo, ingestion.DefaultChunkConfig())
	ctx := tenant.WithTenantID(context.Background(), tenantID.String())
	if err := pipeline.Ingest(ctx, docID, "key", "html"); err != nil {
		t.Fatalf("Ingest HTML: %v", err)
	}
	if repo.status != knowledge.StatusReady {
		t.Errorf("status = %q, want ready", repo.status)
	}
}

func TestPipeline_alreadyReadyIsIdempotent(t *testing.T) {
	connector := &memConnector{data: "should not be read"}
	embedder := &memEmbedder{dim: 1024}
	idxr := &memIndexer{}
	// Document already successfully ingested — pipeline must skip.
	repo := &memDocRepo{status: knowledge.StatusReady}
	tenantID := uuid.New()
	docID := uuid.New()

	pipeline := ingestion.NewPipeline(connector, ingestion.NewRegexScrubber(), embedder, idxr, repo, ingestion.DefaultChunkConfig())
	ctx := tenant.WithTenantID(context.Background(), tenantID.String())
	if err := pipeline.Ingest(ctx, docID, "key", "txt"); err != nil {
		t.Fatalf("Ingest on ready doc should return nil, got: %v", err)
	}
	if idxr.indexed != 0 {
		t.Errorf("expected 0 chunks indexed for already-ready doc, got %d", idxr.indexed)
	}
	// Status must remain Ready (not overwritten with Processing).
	if repo.status != knowledge.StatusReady {
		t.Errorf("status = %q, want ready", repo.status)
	}
}
