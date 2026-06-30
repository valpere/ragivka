package ingestion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Embedder converts text chunks into dense float vectors (FR-9, NFR-8).
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// OllamaEmbedConfig holds connection parameters for the Ollama embed endpoint.
type OllamaEmbedConfig struct {
	// APIURL is the full embed endpoint, e.g. https://ollama.com/api/embed
	APIURL string
	// APIKey is the bearer token for Ollama cloud authentication.
	APIKey string
	// Model is the embedding model, e.g. "bge-m3:latest" (dim 1024).
	Model string
	// ExpectedDim is the expected vector dimension for the model (e.g. 1024 for bge-m3:latest).
	// When non-zero, Embed validates each returned vector and returns an error on mismatch.
	// This guards against silent corruption when model config drifts from schema (VECTOR(1024)).
	ExpectedDim int
}

type ollamaEmbedder struct {
	cfg  OllamaEmbedConfig
	http *http.Client
}

// NewOllamaEmbedder returns an Embedder backed by the Ollama /api/embed endpoint.
func NewOllamaEmbedder(cfg OllamaEmbedConfig) Embedder {
	return &ollamaEmbedder{
		cfg:  cfg,
		http: &http.Client{Timeout: 120 * time.Second},
	}
}

type ollamaEmbedReq struct {
	Model    string   `json:"model"`
	Input    []string `json:"input"`
	Truncate bool     `json:"truncate"`
}

type ollamaEmbedResp struct {
	Embeddings [][]float32 `json:"embeddings"`
	Error      string      `json:"error,omitempty"`
}

// Embed sends a batch of texts to Ollama and returns one embedding vector per text.
// Batch size should be kept ≤ 32 to avoid timeouts on large models.
func (e *ollamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	body := ollamaEmbedReq{
		Model:    e.cfg.Model,
		Input:    texts,
		Truncate: true,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("embedder: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.cfg.APIURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("embedder: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.cfg.APIKey)
	}

	resp, err := e.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedder: http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("embedder: read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedder: status %d: %s", resp.StatusCode, raw)
	}

	var result ollamaEmbedResp
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("embedder: parse response: %w", err)
	}
	if result.Error != "" {
		return nil, fmt.Errorf("embedder: %s", result.Error)
	}
	if len(result.Embeddings) != len(texts) {
		return nil, fmt.Errorf("embedder: expected %d embeddings, got %d", len(texts), len(result.Embeddings))
	}
	if e.cfg.ExpectedDim > 0 {
		for i, vec := range result.Embeddings {
			if len(vec) != e.cfg.ExpectedDim {
				return nil, fmt.Errorf("embedder: vector[%d] has dim %d, expected %d (model=%s)",
					i, len(vec), e.cfg.ExpectedDim, e.cfg.Model)
			}
		}
	}
	return result.Embeddings, nil
}
