package obs

import (
	"context"
	"math"
	"testing"
)

func TestCalculateCost(t *testing.T) {
	tests := []struct {
		model            string
		promptTokens     int
		completionTokens int
		expectedCost     float64
	}{
		{
			model:            "gpt-4o",
			promptTokens:     1000000,
			completionTokens: 1000000,
			expectedCost:     12.50, // 1M prompt ($2.50) + 1M completion ($10.00)
		},
		{
			model:            "gpt-4o-mini",
			promptTokens:     2000000,
			completionTokens: 500000,
			expectedCost:     0.60, // 2M prompt ($0.30) + 0.5M completion ($0.30)
		},
		{
			model:            "unknown-model",
			promptTokens:     1000000,
			completionTokens: 1000000,
			expectedCost:     0.75, // falls back to gpt-4o-mini rates
		},
	}

	const delta = 1e-9
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			cost := CalculateCost(tt.model, tt.promptTokens, tt.completionTokens)
			if math.Abs(cost-tt.expectedCost) > delta {
				t.Errorf("expected cost %f, got %f", tt.expectedCost, cost)
			}
		})
	}
}

func TestLogRequestCost(t *testing.T) {
	ctx := context.Background()
	tenantID := "test-tenant-123"
	model := "gemini-2.5-flash"
	promptTokens := 10000
	completionTokens := 5000

	// Act
	cost := LogRequestCost(ctx, tenantID, model, promptTokens, completionTokens)

	// Assert
	expected := (10000.0/1000000.0)*0.075 + (5000.0/1000000.0)*0.30
	const delta = 1e-9
	if math.Abs(cost-expected) > delta {
		t.Errorf("expected cost %f, got %f", expected, cost)
	}
}
