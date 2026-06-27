package obs

import (
	"context"
	"math"
	"testing"

	"go.opentelemetry.io/otel/trace"
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
			cost, ok := CalculateCost(tt.model, tt.promptTokens, tt.completionTokens)
			if tt.model == "unknown-model" {
				if ok {
					t.Errorf("expected ok to be false for unknown model")
				}
			} else {
				if !ok {
					t.Errorf("expected ok to be true for registered model")
				}
			}
			if math.Abs(cost-tt.expectedCost) > delta {
				t.Errorf("expected cost %f, got %f", tt.expectedCost, cost)
			}
		})
	}

	// Test double fallback when gpt-4o-mini is missing from map
	t.Run("double fallback if gpt-4o-mini missing", func(t *testing.T) {
		oldRegistry := defaultPricingRegistry
		defaultPricingRegistry = map[string]ModelPricing{}
		defer func() { defaultPricingRegistry = oldRegistry }()

		cost, ok := CalculateCost("unknown-model", 1000000, 1000000)
		if ok {
			t.Error("expected ok to be false")
		}
		expectedCost := 0.15 + 0.60 // fallbackPricing rates
		if math.Abs(cost-expectedCost) > delta {
			t.Errorf("expected double fallback cost %f, got %f", expectedCost, cost)
		}
	})
}

func TestLogRequestCost(t *testing.T) {
	tenantID := "test-tenant-123"
	model := "gemini-2.5-flash"
	promptTokens := 10000
	completionTokens := 5000

	// Test case 1: With no trace context
	t.Run("without trace context", func(t *testing.T) {
		ctx := context.Background()
		cost := LogRequestCost(ctx, tenantID, model, promptTokens, completionTokens)

		expected := (10000.0/1000000.0)*0.075 + (5000.0/1000000.0)*0.30
		const delta = 1e-9
		if math.Abs(cost-expected) > delta {
			t.Errorf("expected cost %f, got %f", expected, cost)
		}
	})

	// Test case 2: With trace context
	t.Run("with trace context", func(t *testing.T) {
		traceID, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
		if err != nil {
			t.Fatalf("failed to parse trace ID: %v", err)
		}
		spanContext := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    traceID,
			SpanID:     [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
			TraceFlags: trace.FlagsSampled,
		})
		ctx := trace.ContextWithSpanContext(context.Background(), spanContext)

		// Act & Assert (Should not panic, should extract trace ID successfully)
		cost := LogRequestCost(ctx, tenantID, model, promptTokens, completionTokens)
		if cost <= 0 {
			t.Errorf("expected positive cost, got %f", cost)
		}
	})

	// Test case 3: Unregistered model fallback
	t.Run("unregistered model fallback", func(t *testing.T) {
		ctx := context.Background()
		cost := LogRequestCost(ctx, tenantID, "some-imaginary-model", promptTokens, completionTokens)

		// Should fallback to gpt-4o-mini rates
		expected := (10000.0/1000000.0)*0.15 + (5000.0/1000000.0)*0.60
		const delta = 1e-9
		if math.Abs(cost-expected) > delta {
			t.Errorf("expected cost %f, got %f", expected, cost)
		}
	})
}

func TestGetModelPricing(t *testing.T) {
	tests := []struct {
		model      string
		expectFound bool
	}{
		{"gpt-4o", true},
		{"GPT-4O", true}, // case-insensitivity test
		{"gemini-2.5-flash", true},
		{"invalid-model", false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			_, ok := GetModelPricing(tt.model)
			if ok != tt.expectFound {
				t.Errorf("expected found=%v, got=%v", tt.expectFound, ok)
			}
		})
	}
}
