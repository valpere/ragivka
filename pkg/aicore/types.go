package aicore

import "context"

// Message is a single turn in a conversation, matching the Ollama/OpenAI chat format.
type Message struct {
	Role    string `json:"role"`    // "user" | "assistant" | "system"
	Content string `json:"content"`
}

// TaskKind hints to the router which model tier to select (FR-13).
type TaskKind string

const (
	TaskClassification TaskKind = "classification"
	TaskExtraction     TaskKind = "extraction"
	TaskReasoning      TaskKind = "reasoning"
	TaskGeneration     TaskKind = "generation"
)

// GenerateRequest is the provider-agnostic request to the LLM layer.
type GenerateRequest struct {
	Model       string    // optional override; empty = use router/client default
	Messages    []Message
	MaxTokens   int
	Temperature float64
	ForceJSON   bool     // sets format=json in Ollama; enables structured output (FR-15)
	TaskHint    TaskKind // used by ModelRouter to select the appropriate model (FR-13)
}

// GenerateResponse is the provider-agnostic response from the LLM layer.
type GenerateResponse struct {
	Content      string
	Model        string // model that actually handled the request
	InputTokens  int
	OutputTokens int
}

// LLMClient is the low-level interface for a single LLM provider (NFR-8).
// Each provider (Ollama, OpenAI, Anthropic) implements this interface.
type LLMClient interface {
	Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error)
}

// ModelRouter selects the appropriate model by task and handles fallback (FR-13).
type ModelRouter interface {
	Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error)
}

// PromptRegistry provides version-controlled access to system prompts (FR-14).
type PromptRegistry interface {
	Load(ctx context.Context, name string, version int) (string, error)
	LoadLatest(ctx context.Context, name string) (string, error)
}
