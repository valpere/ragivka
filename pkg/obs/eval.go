package obs

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// retrievalRecallAtK is a histogram of Recall@K scores per tenant (NFR-14).
	// Buckets span [0, 1] in 0.1 steps so offline analysis can compute percentiles.
	retrievalRecallAtK = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ragivka_retrieval_recall_at_k",
		Help:    "Retrieval Recall@K score per query (offline evaluation hook, NFR-14).",
		Buckets: prometheus.LinearBuckets(0, 0.1, 11),
	}, []string{"tenant_id"})

	// citationCoverage is a per-tenant gauge of the latest citation coverage score (NFR-14).
	citationCoverage = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ragivka_citation_coverage",
		Help: "Fraction of response sentences with at least one citation (NFR-14).",
	}, []string{"tenant_id"})
)

// GroundednessHook scores a generated response against its retrieved chunks.
// Called post-generation; result is logged only — not used to gate responses in Phase 2 (NFR-14).
// Runtime blocking is deferred to Phase 3.
type GroundednessHook interface {
	Check(ctx context.Context, response string, chunks []string) (score float64, err error)
}

// LogRetrievalQuality records Recall@K for a single retrieval query (NFR-14).
// recallAtK is the fraction of topK slots filled by scored chunks; callers supply this value.
// The log entry is structured JSON to stdout for offline evaluation pipelines.
func LogRetrievalQuality(ctx context.Context, tenantID string, queryID uuid.UUID, topK int, recallAtK float64) {
	retrievalRecallAtK.WithLabelValues(tenantID).Observe(recallAtK)
	slog.InfoContext(ctx, "retrieval_quality",
		"tenant_id", tenantID,
		"query_id", queryID,
		"top_k", topK,
		"recall_at_k", recallAtK,
	)
}

// LogCitationCoverage records citation coverage for a session response (NFR-14).
// coverage is the fraction of response sentences backed by at least one retrieved chunk, in [0, 1].
// Phase 2 callers may supply a binary proxy (1.0 if any citations present, 0.0 otherwise).
func LogCitationCoverage(ctx context.Context, tenantID string, sessionID uuid.UUID, coverage float64) {
	citationCoverage.WithLabelValues(tenantID).Set(coverage)
	slog.InfoContext(ctx, "citation_coverage",
		"tenant_id", tenantID,
		"session_id", sessionID,
		"coverage", coverage,
	)
}
