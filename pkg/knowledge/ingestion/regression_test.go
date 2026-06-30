package ingestion_test

// regression_test.go — tests that cover every confirmed fix-review finding for PR #25,
// plus unit tests for embedder, S3Connector, and IngestDocumentWorker.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/valpere/ragivka/pkg/knowledge"
	"github.com/valpere/ragivka/pkg/knowledge/ingestion"
	"github.com/valpere/ragivka/pkg/tenant"
)

// ---------------------------------------------------------------------------
// REGRESSION: MaxDocumentBytes (PR #25 fix — io.LimitReader cap)
// ---------------------------------------------------------------------------

// oversizeConnector returns a reader that always reports more bytes than MaxDocumentBytes.
type oversizeConnector struct{}

func (o *oversizeConnector) Connect(_ context.Context, _ string) (io.ReadCloser, error) {
	// Serve exactly MaxDocumentBytes+1 bytes so the limit is exceeded.
	size := ingestion.MaxDocumentBytes + 1
	return io.NopCloser(strings.NewReader(strings.Repeat("x", int(size)))), nil
}

func TestPipeline_oversizedDocumentReturnsError(t *testing.T) {
	repo := &memDocRepo{status: knowledge.StatusPending}
	pipeline := ingestion.NewPipeline(
		&oversizeConnector{},
		ingestion.NewRegexScrubber(),
		&memEmbedder{dim: 1024},
		&memIndexer{},
		repo,
		ingestion.DefaultChunkConfig(),
	)
	ctx := tenant.WithTenantID(context.Background(), uuid.New().String())
	err := pipeline.Ingest(ctx, uuid.New(), "key", "txt")
	if err == nil {
		t.Fatal("expected error for document exceeding MaxDocumentBytes, got nil")
	}
	if repo.status != knowledge.StatusFailed {
		t.Errorf("document status = %q, want failed", repo.status)
	}
}

// ---------------------------------------------------------------------------
// REGRESSION: Pipeline already-Ready guard (PR #25 fix)
// memDocRepo and TestPipeline_alreadyReadyIsIdempotent are in ingestion_test.go.
// Here we add: processing a Failed document must re-attempt (not skip).
// ---------------------------------------------------------------------------

func TestPipeline_failedDocumentIsRetried(t *testing.T) {
	text := strings.Repeat("retry content ", 20)
	idxr := &memIndexer{}
	repo := &memDocRepo{status: knowledge.StatusFailed}

	pipeline := ingestion.NewPipeline(
		&memConnector{data: text},
		ingestion.NewRegexScrubber(),
		&memEmbedder{dim: 1024},
		idxr,
		repo,
		ingestion.DefaultChunkConfig(),
	)
	ctx := tenant.WithTenantID(context.Background(), uuid.New().String())
	if err := pipeline.Ingest(ctx, uuid.New(), "key", "txt"); err != nil {
		t.Fatalf("Ingest retry failed: %v", err)
	}
	if repo.status != knowledge.StatusReady {
		t.Errorf("status = %q after retry, want ready", repo.status)
	}
	if idxr.indexed == 0 {
		t.Error("expected chunks to be indexed on retry")
	}
}

// ---------------------------------------------------------------------------
// REGRESSION: Embedding dimension validation (PR #25 fix — ExpectedDim field)
// ---------------------------------------------------------------------------

// newFakeEmbedServer spins up an httptest.Server that returns `dim`-dimensional
// zero vectors, or `wrongDim` if wrongDim > 0.
func newFakeEmbedServer(t *testing.T, dim int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		embeddings := make([][]float32, len(req.Input))
		for i := range embeddings {
			embeddings[i] = make([]float32, dim)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": embeddings})
	}))
}

func TestOllamaEmbedder_dimensionMismatchRejected(t *testing.T) {
	// Server returns dim=512 but we expect dim=1024 → must error.
	srv := newFakeEmbedServer(t, 512)
	defer srv.Close()

	e := ingestion.NewOllamaEmbedder(ingestion.OllamaEmbedConfig{
		APIURL:      srv.URL,
		Model:       "bge-m3:latest",
		ExpectedDim: 1024,
	})
	_, err := e.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected dimension mismatch error, got nil")
	}
}

func TestOllamaEmbedder_correctDimAccepted(t *testing.T) {
	srv := newFakeEmbedServer(t, 1024)
	defer srv.Close()

	e := ingestion.NewOllamaEmbedder(ingestion.OllamaEmbedConfig{
		APIURL:      srv.URL,
		Model:       "bge-m3:latest",
		ExpectedDim: 1024,
	})
	vecs, err := e.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 embeddings, got %d", len(vecs))
	}
	if len(vecs[0]) != 1024 {
		t.Errorf("embedding dim = %d, want 1024", len(vecs[0]))
	}
}

func TestOllamaEmbedder_emptyInputReturnsNil(t *testing.T) {
	srv := newFakeEmbedServer(t, 1024)
	defer srv.Close()

	e := ingestion.NewOllamaEmbedder(ingestion.OllamaEmbedConfig{APIURL: srv.URL, Model: "bge-m3:latest"})
	vecs, err := e.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("Embed nil: %v", err)
	}
	if vecs != nil {
		t.Errorf("expected nil for empty input, got %v", vecs)
	}
}

func TestOllamaEmbedder_batchCountMismatch(t *testing.T) {
	// Server returns fewer embeddings than texts — must error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{make([]float32, 1024)}, // only 1 for 2 inputs
		})
	}))
	defer srv.Close()

	e := ingestion.NewOllamaEmbedder(ingestion.OllamaEmbedConfig{APIURL: srv.URL, Model: "x"})
	_, err := e.Embed(context.Background(), []string{"a", "b"})
	if err == nil {
		t.Fatal("expected error on batch count mismatch, got nil")
	}
}

func TestOllamaEmbedder_serverError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"model not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	e := ingestion.NewOllamaEmbedder(ingestion.OllamaEmbedConfig{APIURL: srv.URL, Model: "x"})
	_, err := e.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
}

func TestOllamaEmbedder_noDimCheckWhenZero(t *testing.T) {
	// ExpectedDim=0 means no validation — any dimension is accepted.
	srv := newFakeEmbedServer(t, 256)
	defer srv.Close()

	e := ingestion.NewOllamaEmbedder(ingestion.OllamaEmbedConfig{
		APIURL:      srv.URL,
		Model:       "small-model",
		ExpectedDim: 0,
	})
	vecs, err := e.Embed(context.Background(), []string{"x"})
	if err != nil {
		t.Fatalf("expected no error with ExpectedDim=0, got: %v", err)
	}
	if len(vecs[0]) != 256 {
		t.Errorf("dim = %d, want 256", len(vecs[0]))
	}
}

// ---------------------------------------------------------------------------
// S3Connector
// ---------------------------------------------------------------------------

// mockPresigner implements storage.StorageClient returning a preset URL for PresignURL.
type mockPresigner struct{ url string }

func (m *mockPresigner) PutObject(_ context.Context, _ string, _ io.Reader, _ int64) error {
	return nil
}
func (m *mockPresigner) PresignURL(_ context.Context, _ string, _ time.Duration) (string, error) {
	return m.url, nil
}
func (m *mockPresigner) DeleteObject(_ context.Context, _ string) error { return nil }

func TestS3Connector_downloadsContent(t *testing.T) {
	body := "document content from S3"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, body)
	}))
	defer srv.Close()

	c := ingestion.NewS3Connector(&mockPresigner{url: srv.URL})
	rc, err := c.Connect(context.Background(), "some/key")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != body {
		t.Errorf("got %q, want %q", data, body)
	}
}

func TestS3Connector_nonOKStatusReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	c := ingestion.NewS3Connector(&mockPresigner{url: srv.URL})
	_, err := c.Connect(context.Background(), "key")
	if err == nil {
		t.Fatal("expected error for non-200 status, got nil")
	}
}

func TestS3Connector_presignErrorPropagates(t *testing.T) {
	failing := &failPresigner{}
	c := ingestion.NewS3Connector(failing)
	_, err := c.Connect(context.Background(), "key")
	if err == nil {
		t.Fatal("expected error when PresignURL fails")
	}
}

type failPresigner struct{}

func (f *failPresigner) PutObject(_ context.Context, _ string, _ io.Reader, _ int64) error {
	return nil
}
func (f *failPresigner) PresignURL(_ context.Context, _ string, _ time.Duration) (string, error) {
	return "", fmt.Errorf("presign: service unavailable")
}
func (f *failPresigner) DeleteObject(_ context.Context, _ string) error { return nil }

// ---------------------------------------------------------------------------
// vectorLiteral (regression: was pgvector.NewVector; now text format for ::vector cast)
// ---------------------------------------------------------------------------

func TestVectorLiteral_empty(t *testing.T) {
	got := ingestion.VectorLiteral(nil)
	if got != "[]" {
		t.Errorf("VectorLiteral(nil) = %q, want []", got)
	}
}

func TestVectorLiteral_singleValue(t *testing.T) {
	got := ingestion.VectorLiteral([]float32{0.5})
	if got != "[0.5]" {
		t.Errorf("got %q, want [0.5]", got)
	}
}

func TestVectorLiteral_multipleValues(t *testing.T) {
	got := ingestion.VectorLiteral([]float32{1.0, -0.5, 0.0})
	if got != "[1,-0.5,0]" {
		t.Errorf("got %q, want [1,-0.5,0]", got)
	}
}

func TestVectorLiteral_roundtripLength(t *testing.T) {
	v := make([]float32, 1024)
	for i := range v {
		v[i] = float32(i) / 1000.0
	}
	lit := ingestion.VectorLiteral(v)
	if !strings.HasPrefix(lit, "[") || !strings.HasSuffix(lit, "]") {
		t.Errorf("vectorLiteral not wrapped in brackets: %q", lit[:20])
	}
	// Count commas → should be 1023 for 1024 elements
	commas := strings.Count(lit, ",")
	if commas != 1023 {
		t.Errorf("expected 1023 commas for 1024 elements, got %d", commas)
	}
}

// ---------------------------------------------------------------------------
// IngestDocumentWorker
// ---------------------------------------------------------------------------

// mockIngester implements Ingester for worker tests.
type mockIngester struct {
	called bool
	docID  uuid.UUID
	err    error
}

func (m *mockIngester) Ingest(_ context.Context, docID uuid.UUID, _, _ string) error {
	m.called = true
	m.docID = docID
	return m.err
}

func TestIngestDocumentWorker_nilTenantIDReturnsError(t *testing.T) {
	w := ingestion.NewIngestDocumentWorker(&mockIngester{})
	job := &river.Job[ingestion.IngestDocumentArgs]{
		Args: ingestion.IngestDocumentArgs{
			TenantID:   uuid.Nil, // missing
			DocumentID: uuid.New(),
			S3Key:      "key",
			DocType:    "txt",
		},
	}
	err := w.Work(context.Background(), job)
	if err == nil {
		t.Fatal("expected error for nil TenantID, got nil")
	}
}

func TestIngestDocumentWorker_callsPipelineWithTenantContext(t *testing.T) {
	mock := &mockIngester{}
	w := ingestion.NewIngestDocumentWorker(mock)
	docID := uuid.New()
	tenantID := uuid.New()

	job := &river.Job[ingestion.IngestDocumentArgs]{
		Args: ingestion.IngestDocumentArgs{
			TenantID:   tenantID,
			DocumentID: docID,
			S3Key:      "tenant/doc.txt",
			DocType:    "txt",
		},
	}
	if err := w.Work(context.Background(), job); err != nil {
		t.Fatalf("Work: %v", err)
	}
	if !mock.called {
		t.Error("expected pipeline.Ingest to be called")
	}
	if mock.docID != docID {
		t.Errorf("docID = %v, want %v", mock.docID, docID)
	}
}

func TestIngestDocumentWorker_pipelineErrorPropagates(t *testing.T) {
	mock := &mockIngester{err: fmt.Errorf("pipeline exploded")}
	w := ingestion.NewIngestDocumentWorker(mock)
	job := &river.Job[ingestion.IngestDocumentArgs]{
		Args: ingestion.IngestDocumentArgs{
			TenantID:   uuid.New(),
			DocumentID: uuid.New(),
			S3Key:      "key",
			DocType:    "txt",
		},
	}
	err := w.Work(context.Background(), job)
	if err == nil {
		t.Fatal("expected propagated error, got nil")
	}
}

func TestIngestDocumentArgs_kind(t *testing.T) {
	if got := (ingestion.IngestDocumentArgs{}).Kind(); got != "ingest_document" {
		t.Errorf("Kind() = %q, want ingest_document", got)
	}
}
