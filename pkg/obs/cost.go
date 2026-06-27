package obs

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"
)

// ModelPricing defines token rates per 1,000,000 tokens in USD.
type ModelPricing struct {
	InputRate  float64 // Cost per 1M prompt tokens
	OutputRate float64 // Cost per 1M completion tokens
}

// DefaultPricingRegistry holds baseline pricing for standard models as of 2026.
// NFR-13 Cost Tracking
var DefaultPricingRegistry = map[string]ModelPricing{
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
}

// CostLogEntry represents the structured audit log for cost tracking.
type CostLogEntry struct {
	Timestamp        string  `json:"timestamp"`
	TenantID         string  `json:"tenant_id"`
	Model            string  `json:"model"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CostUSD          float64 `json:"cost_usd"`
}

// CalculateCost computes cost in USD based on model pricing.
func CalculateCost(model string, promptTokens, completionTokens int) float64 {
	pricing, ok := DefaultPricingRegistry[strings.ToLower(model)]
	if !ok {
		// Fallback to a cheap default if model is not registered (e.g. gpt-4o-mini)
		pricing = DefaultPricingRegistry["gpt-4o-mini"]
	}

	promptCost := (float64(promptTokens) / 1000000.0) * pricing.InputRate
	completionCost := (float64(completionTokens) / 1000000.0) * pricing.OutputRate

	return promptCost + completionCost
}

// LogRequestCost calculates, logs, and increments Prometheus counters for the request's token cost.
// NFR-13 Cost Tracking & NFR-12 Metrics
func LogRequestCost(ctx context.Context, tenantID, model string, promptTokens, completionTokens int) float64 {
	cost := CalculateCost(model, promptTokens, completionTokens)

	// Increment metrics
	RecordLLMTokenUsage(tenantID, model, promptTokens, completionTokens)
	TenantCosts.WithLabelValues(tenantID, model).Add(cost)

	// Create structured audit log
	entry := CostLogEntry{
		Timestamp:        time.Now().UTC().Format(time.RFC3339),
		TenantID:         tenantID,
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
