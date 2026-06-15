package gateway

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// metricsRegistry holds the gateway's Prometheus collectors. We use a
// dedicated `*prometheus.Registry` rather than the global default so test
// runs do not collide with the implicit default registry, and so the surface
// is explicitly enumerable from one place.
var (
	metricsRegistry = prometheus.NewRegistry()

	kmsHTTPRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "kms_http_requests_total",
		Help: "Total HTTP requests by matched route, method, and status.",
	}, []string{"path", "method", "status"})

	kmsHTTPRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "kms_http_request_duration_seconds",
		Help:    "HTTP request latency by matched route and method.",
		Buckets: prometheus.DefBuckets,
	}, []string{"path", "method"})

	kmsSigningDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "kms_signing_duration_seconds",
		Help:    "Wall-clock duration of the signing operation, by chain.",
		Buckets: prometheus.DefBuckets,
	}, []string{"chain"})

	kmsRateLimitRejectionsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "kms_rate_limit_rejections_total",
		Help: "Total HTTP 429 responses by matched route.",
	}, []string{"path"})

	kmsVaultCallsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "kms_vault_calls_total",
		Help: "Vault calls by operation and outcome class.",
	}, []string{"op", "status"})

	kmsVaultCallDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "kms_vault_call_duration_seconds",
		Help:    "Vault call latency by operation.",
		Buckets: prometheus.DefBuckets,
	}, []string{"op"})

	kmsTokenRenewalFailuresTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "kms_token_renewal_failures_total",
		Help: "Total Vault token renewal failures observed by the renewal goroutine.",
	})

	kmsPanicsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "kms_panics_total",
		Help: "Total handler panics caught by the recover middleware, by matched route.",
	}, []string{"path"})
)

func init() {
	metricsRegistry.MustRegister(
		kmsHTTPRequestsTotal,
		kmsHTTPRequestDuration,
		kmsSigningDuration,
		kmsRateLimitRejectionsTotal,
		kmsVaultCallsTotal,
		kmsVaultCallDuration,
		kmsTokenRenewalFailuresTotal,
		kmsPanicsTotal,
	)
}

// MetricsRegistry exposes the gateway's metrics registry so callers (e.g.
// integration tests, custom embeddings) can scrape it directly.
func MetricsRegistry() *prometheus.Registry { return metricsRegistry }

// IncrementTokenRenewalFailure bumps the kms_token_renewal_failures_total
// counter. Exposed as a package-level function so callers outside the
// gateway (e.g. internal/vault's renewal goroutine) can wire it up via
// `vault.Client.SetRenewalFailureHook` without depending on the prometheus
// dep directly.
func IncrementTokenRenewalFailure() { kmsTokenRenewalFailuresTotal.Inc() }

// ObserveSigningDuration records the latency of a signing operation
// under the kms_signing_duration_seconds histogram. `chain` is "evm" or
// "cosmos"; the label set is bounded.
func ObserveSigningDuration(chain string, seconds float64) {
	kmsSigningDuration.WithLabelValues(chain).Observe(seconds)
}

// ObserveVaultCall records a Vault call's outcome under
// kms_vault_calls_total and its latency under kms_vault_call_duration_seconds.
// `status` is one of "ok", "permission_denied", "not_found", "error".
func ObserveVaultCall(op, status string, seconds float64) {
	kmsVaultCallsTotal.WithLabelValues(op, status).Inc()
	kmsVaultCallDuration.WithLabelValues(op).Observe(seconds)
}

// metricsHandler returns the /metrics http.Handler bound to our registry.
func metricsHandler() http.Handler {
	return promhttp.HandlerFor(metricsRegistry, promhttp.HandlerOpts{Registry: metricsRegistry})
}
