package obs

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"
)

// ModelPricing defines token rates per 1,000,000 tokens in USD.
type ModelPricing struct {
	InputRate  float64 // Cost per 1M prompt tokens
	OutputRate float64 // Cost per 1M completion tokens
}

// defaultPricingRegistry holds baseline pricing for standard models as of 2026.
// unexported to prevent concurrent write data races (Review finding)
var defaultPricingRegistry = map[string]ModelPricing{
	"gpt-4o": {
		InputRate:  2.50,
		OutputRate: 10.00,
	},
	"gpt-4o-mini": {
		InputRate:  0.15,
		OutputRate: 0.60,
	},
	"claude-3-5-sonnet": {
		InputRate:  3.00,
		OutputRate: 15.00,
	},
	"claude-3-5-haiku": {
		InputRate:  0.80,
		OutputRate: 4.00,
	},
	"gemini-2.5-flash": {
		InputRate:  0.075,
		OutputRate: 0.30,
	},
	"deepseek-chat": {
		InputRate:  0.14,
		OutputRate: 0.28,
	},
	"deepseek-reasoner": {
		InputRate:  0.55,
		OutputRate: 2.19,
	},
	// Ollama-hosted models billed via subscription, not per-token.
	// Zero rates ensure LogRequestCost tracks usage without inflated cost estimates.
	"qwen3.5:cloud": {
		InputRate:  0.0,
		OutputRate: 0.0,
	},
	"bge-m3:latest": {
		InputRate:  0.0,
		OutputRate: 0.0,
	},
}

var fallbackPricing = ModelPricing{
	InputRate:  0.15,
	OutputRate: 0.60,
}

// GetModelPricing returns a copy of pricing for the model (thread-safe read).
// It returns the ModelPricing struct and a boolean indicating if the model was found.
// If not found, it returns a zero-valued ModelPricing and false.
func GetModelPricing(model string) (ModelPricing, bool) {
	pricing, ok := defaultPricingRegistry[strings.ToLower(model)]
	return pricing, ok
}

// CostLogEntry represents the structured audit log for cost tracking.
type CostLogEntry struct {
	Timestamp        string  `json:"timestamp"`
	TenantID         string  `json:"tenant_id"`
	TraceID          string  `json:"trace_id,omitempty"` // Trace ID for cross-system correlation
	Model            string  `json:"model"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CostUSD          float64 `json:"cost_usd"`
}

// CalculateCost computes cost in USD based on model pricing.
// It returns the calculated cost and a boolean indicating if the model was registered.
func CalculateCost(model string, promptTokens, completionTokens int) (float64, bool) {
	pricing, ok := GetModelPricing(model)
	if !ok {
		// Fallback to gpt-4o-mini rates
		var okFallback bool
		pricing, okFallback = GetModelPricing("gpt-4o-mini")
		if !okFallback {
			// Hardcoded safe default rates if registry itself is corrupted
			pricing = fallbackPricing
		}
	}

	promptCost := (float64(promptTokens) / 1000000.0) * pricing.InputRate
	completionCost := (float64(completionTokens) / 1000000.0) * pricing.OutputRate

	return promptCost + completionCost, ok
}

// LogRequestCost calculates, logs, and increments Prometheus counters for the request's token cost.
// NFR-13 Cost Tracking & NFR-12 Metrics
func LogRequestCost(ctx context.Context, tenantID, model string, promptTokens, completionTokens int) float64 {
	cost, ok := CalculateCost(model, promptTokens, completionTokens)

	// Increment metrics
	RecordLLMTokenUsage(tenantID, model, promptTokens, completionTokens)
	TenantCosts.WithLabelValues(tenantID, model).Add(cost)

	// Log warning for unregistered model at the call-site if not registered
	if !ok {
		log.Printf("WARNING: Model %q not found in pricing registry, using gpt-4o-mini rates", model)
	}

	// Extract Trace ID from context
	var traceID string
	spanContext := trace.SpanContextFromContext(ctx)
	if spanContext.IsValid() {
		traceID = spanContext.TraceID().String()
	}

	// Create structured audit log
	entry := CostLogEntry{
		Timestamp:        time.Now().UTC().Format(time.RFC3339),
		TenantID:         tenantID,
		TraceID:          traceID,
		Model:            model,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		CostUSD:          cost,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("ERROR: Failed to serialize cost log: %v", err)
	} else {
		// Structured log goes to stdout/logs for collection and budget enforcement
		log.Println(string(data))
	}

	return cost
}
