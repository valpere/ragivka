package obs_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/valpere/ragivka/pkg/obs"
)

// TestInitTracerNoOp verifies that InitTracer with an empty endpoint returns no
// error and provides a callable shutdown function. NFR-11.
func TestInitTracerNoOp(t *testing.T) {
	ctx := context.Background()
	shutdown, err := obs.InitTracer(ctx, "test-service", "")
	if err != nil {
		t.Fatalf("InitTracer with empty endpoint returned error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("InitTracer returned nil shutdown function")
	}
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown func returned error: %v", err)
	}
}

// TestInitTracerInsecureEnvNoOp verifies that OTEL_EXPORTER_OTLP_INSECURE=true
// with an empty endpoint still takes the no-op path without panicking. NFR-11.
func TestInitTracerInsecureEnvNoOp(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "true")
	ctx := context.Background()
	shutdown, err := obs.InitTracer(ctx, "test-service-insecure", "")
	if err != nil {
		t.Fatalf("InitTracer (insecure env, empty endpoint) returned error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("InitTracer returned nil shutdown function")
	}
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown func returned error: %v", err)
	}
}

// TestMetricsHandlerHTTP verifies MetricsHandler returns HTTP 200 with a
// text/plain Content-Type suitable for Prometheus scraping. NFR-12.
func TestMetricsHandlerHTTP(t *testing.T) {
	handler := obs.MetricsHandler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("expected Content-Type to contain text/plain, got %q", ct)
	}
}

// TestMetricsHandlerReflectsRecordedData verifies that after calling
// RecordRetrievalLatency and LogRequestCost the corresponding metric names
// appear in the /metrics response body. NFR-12/NFR-13.
func TestMetricsHandlerReflectsRecordedData(t *testing.T) {
	ctx := context.Background()
	tenantID := "tenant-obs-reflect"
	model := "gpt-4o-mini"

	obs.RecordRetrievalLatency(tenantID, 42*time.Millisecond)
	obs.LogRequestCost(ctx, tenantID, model, 500, 100)

	handler := obs.MetricsHandler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	body := rr.Body.String()
	want := []string{
		"ragivka_retrieval_latency_seconds",
		"ragivka_llm_tokens_total",
		"ragivka_tenant_cost_usd_total",
	}
	for _, name := range want {
		if !strings.Contains(body, name) {
			t.Errorf("metric %q not found in /metrics output", name)
		}
	}
}

// TestLogRequestCostUnregisteredModelRegression is a regression test confirming
// that an unknown model name falls back to gpt-4o-mini rates without panicking
// and returns a non-negative cost. NFR-13.
func TestLogRequestCostUnregisteredModelRegression(t *testing.T) {
	ctx := context.Background()
	cost := obs.LogRequestCost(ctx, "tenant-regression", "totally-unknown-model-xyz", 1000, 500)
	if cost < 0 {
		t.Errorf("expected non-negative fallback cost, got %f", cost)
	}
}
