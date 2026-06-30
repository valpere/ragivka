package aicore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OllamaConfig holds connection parameters for the Ollama API endpoint.
type OllamaConfig struct {
	// APIURL is the full chat endpoint, e.g. https://ollama.com/api/chat
	APIURL string
	// APIKey is the bearer token for Ollama cloud authentication.
	APIKey string
	// Model is the default model, e.g. "qwen3.5:cloud" or "bge-m3:latest".
	Model string
}

type ollamaClient struct {
	cfg  OllamaConfig
	http *http.Client
}

// NewOllamaClient creates a new Ollama HTTP client.
// The underlying http.Client times out at 120 s to handle slow cloud inference.
func NewOllamaClient(cfg OllamaConfig) LLMClient {
	return &ollamaClient{
		cfg:  cfg,
		http: &http.Client{Timeout: 120 * time.Second},
	}
}

// ollamaReq is the JSON body sent to the Ollama chat endpoint.
type ollamaReq struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
	Format   string    `json:"format,omitempty"` // "json" for structured output (FR-15)
}

// ollamaResp is the JSON body returned by the Ollama chat endpoint.
type ollamaResp struct {
	Model           string  `json:"model"`
	Message         Message `json:"message"`
	PromptEvalCount int     `json:"prompt_eval_count"`
	EvalCount       int     `json:"eval_count"`
	Error           string  `json:"error,omitempty"`
}

func (c *ollamaClient) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	model := req.Model
	if model == "" {
		model = c.cfg.Model
	}

	body := ollamaReq{
		Model:    model,
		Messages: req.Messages,
		Stream:   false,
	}
	if req.ForceJSON {
		body.Format = "json"
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("ollama: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.APIURL, bytes.NewReader(payload))
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("ollama: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("ollama: http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("ollama: read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return GenerateResponse{}, fmt.Errorf("ollama: status %d: %s", resp.StatusCode, raw)
	}

	var ollamaR ollamaResp
	if err := json.Unmarshal(raw, &ollamaR); err != nil {
		return GenerateResponse{}, fmt.Errorf("ollama: parse response: %w", err)
	}
	if ollamaR.Error != "" {
		return GenerateResponse{}, fmt.Errorf("ollama: %s", ollamaR.Error)
	}

	return GenerateResponse{
		Content:      ollamaR.Message.Content,
		Model:        ollamaR.Model,
		InputTokens:  ollamaR.PromptEvalCount,
		OutputTokens: ollamaR.EvalCount,
	}, nil
}
