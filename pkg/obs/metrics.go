package obs

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// LLMTokenUsage tracks prompt and completion tokens consumed per tenant/model.
	// NFR-12 Metrics
	LLMTokenUsage = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ragivka_llm_tokens_total",
			Help: "Total number of LLM tokens consumed.",
		},
		[]string{"tenant_id", "model", "token_type"}, // token_type: prompt, completion
	)

	// RetrievalLatency tracks context search and chunk retrieval latency.
	// NFR-12 Metrics
	RetrievalLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ragivka_retrieval_latency_seconds",
			Help:    "Time spent performing similarity search and RAG retrieval.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"tenant_id"},
	)

	// RiverQueueDepth tracks the depth of background job queues.
	// NFR-12 Metrics
	RiverQueueDepth = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ragivka_river_queue_depth",
			Help: "Current depth of background river job queues.",
		},
		[]string{"queue_name"},
	)

	// ErrorRates tracks system errors partitioned by component and code.
	// NFR-12 Metrics
	ErrorRates = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ragivka_errors_total",
			Help: "Total number of system errors.",
		},
		[]string{"tenant_id", "component", "error_code"}, // component: api, db, ai, rag
	)

	// TenantCosts tracks running token cost accrued per tenant.
	// NFR-13 Cost Tracking
	TenantCosts = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ragivka_tenant_cost_usd_total",
			Help: "Total token cost accrued per tenant in USD.",
		},
		[]string{"tenant_id", "model"},
	)
)

// RecordLLMTokenUsage records prompt and completion token counts.
func RecordLLMTokenUsage(tenantID, model string, promptTokens, completionTokens int) {
	LLMTokenUsage.WithLabelValues(tenantID, model, "prompt").Add(float64(promptTokens))
	LLMTokenUsage.WithLabelValues(tenantID, model, "completion").Add(float64(completionTokens))
}

// RecordRetrievalLatency records RAG retrieval latency.
func RecordRetrievalLatency(tenantID string, duration time.Duration) {
	RetrievalLatency.WithLabelValues(tenantID).Observe(duration.Seconds())
}

// RecordRiverQueueDepth updates current queue depth.
func RecordRiverQueueDepth(queueName string, depth float64) {
	RiverQueueDepth.WithLabelValues(queueName).Set(depth)
}

// RecordError increments the error metric.
func RecordError(tenantID, component, errorCode string) {
	ErrorRates.WithLabelValues(tenantID, component, errorCode).Inc()
}

// MetricsHandler returns the HTTP handler for Prometheus scrapes.
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}
