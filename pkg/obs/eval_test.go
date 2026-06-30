package obs_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/valpere/ragivka/pkg/obs"
)

func TestLogRetrievalQuality_recordsHistogram(t *testing.T) {
	tenantID := "tenant-eval-recall"
	obs.LogRetrievalQuality(context.Background(), tenantID, uuid.New(), 5, 0.8)

	body := scrapeMetrics(t)
	if !strings.Contains(body, "ragivka_retrieval_recall_at_k") {
		t.Error("ragivka_retrieval_recall_at_k not found in /metrics output")
	}
	if !strings.Contains(body, `tenant_id="tenant-eval-recall"`) {
		t.Errorf("expected tenant label tenant-eval-recall in recall@k metric, not found")
	}
}

func TestLogRetrievalQuality_zeroTopK(t *testing.T) {
	// topK=0 must not panic (recallAtK caller-supplied as 0.0).
	obs.LogRetrievalQuality(context.Background(), "tenant-eval-zero-topk", uuid.New(), 0, 0.0)
}

func TestLogCitationCoverage_recordsGauge(t *testing.T) {
	tenantID := "tenant-eval-coverage"
	obs.LogCitationCoverage(context.Background(), tenantID, uuid.New(), 0.75)

	body := scrapeMetrics(t)
	if !strings.Contains(body, "ragivka_citation_coverage") {
		t.Error("ragivka_citation_coverage not found in /metrics output")
	}
	if !strings.Contains(body, `tenant_id="tenant-eval-coverage"`) {
		t.Errorf("expected tenant label tenant-eval-coverage in citation coverage metric, not found")
	}
}

func TestLogCitationCoverage_zeroIsValid(t *testing.T) {
	// coverage=0.0 (no citations) must not panic and must be recorded.
	tenantID := "tenant-eval-zero-cov"
	obs.LogCitationCoverage(context.Background(), tenantID, uuid.New(), 0.0)

	body := scrapeMetrics(t)
	if !strings.Contains(body, `tenant_id="tenant-eval-zero-cov"`) {
		t.Errorf("zero-coverage recording dropped — tenant label not found in /metrics")
	}
}

// TestGroundednessHook_interface verifies that GroundednessHook can be implemented
// and called without error.  Phase 2 hooks are logged-only; this test covers the
// interface contract (NFR-14).
func TestGroundednessHook_interface(t *testing.T) {
	var hook obs.GroundednessHook = &noopGroundednessHook{}
	score, err := hook.Check(context.Background(), "The answer is 42.", []string{"chunk 1", "chunk 2"})
	if err != nil {
		t.Fatalf("unexpected error from GroundednessHook.Check: %v", err)
	}
	if score < 0 || score > 1 {
		t.Errorf("score out of [0,1] range: %f", score)
	}
}

// noopGroundednessHook is a minimal GroundednessHook implementation for testing.
type noopGroundednessHook struct{}

func (noopGroundednessHook) Check(_ context.Context, _ string, _ []string) (float64, error) {
	return 1.0, nil
}

// scrapeMetrics is a test helper that calls MetricsHandler and returns the body.
func scrapeMetrics(t *testing.T) string {
	t.Helper()
	handler := obs.MetricsHandler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("MetricsHandler returned status %d", rr.Code)
	}
	return rr.Body.String()
}
