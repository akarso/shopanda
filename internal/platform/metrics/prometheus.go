package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// PrometheusRecorder implements Recorder using prometheus/client_golang,
// registered on a private *prometheus.Registry rather than the global
// default registry so multiple instances (e.g. in tests) never collide.
type PrometheusRecorder struct {
	httpRequests   *prometheus.CounterVec
	httpDuration   *prometheus.HistogramVec
	checkoutResult *prometheus.CounterVec
	jobFailures    *prometheus.CounterVec
	webhookResult  *prometheus.CounterVec
	rateLimitErr   *prometheus.CounterVec
	fpcRequests    *prometheus.CounterVec
	fpcRender      *prometheus.HistogramVec
	fpcPurge       *prometheus.CounterVec
	fpcBackendGet  prometheus.Counter
}

// NewPrometheusRecorder creates a PrometheusRecorder and the registry its
// metrics are registered on. Pass the registry to Handler to expose scrapes.
func NewPrometheusRecorder() (*PrometheusRecorder, *prometheus.Registry) {
	reg := prometheus.NewRegistry()
	r := &PrometheusRecorder{
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "shopanda_http_requests_total",
			Help: "Total HTTP requests, labelled by route template, method, and status class.",
		}, []string{"route", "method", "status_class"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "shopanda_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds, labelled by route template and method.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route", "method"}),
		checkoutResult: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "shopanda_checkout_result_total",
			Help: "Total checkout attempts, labelled by outcome (success/failed).",
		}, []string{"outcome"}),
		jobFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "shopanda_job_failures_total",
			Help: "Total background job failures, labelled by job type.",
		}, []string{"job_type"}),
		webhookResult: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "shopanda_webhook_deliveries_total",
			Help: "Total outbound webhook delivery attempts, labelled by outcome (success/failed).",
		}, []string{"outcome"}),
		rateLimitErr: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "shopanda_ratelimit_backend_errors_total",
			Help: "Total remote rate-limit backend errors, labelled by limiter name and reason (error, circuit_open, or pool_timeout).",
		}, []string{"limiter", "reason"}),
		fpcRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "shopanda_fpc_requests_total",
			Help: "Full-page cache decisions, labelled by route template and outcome (hit/miss/bypass).",
		}, []string{"route", "outcome"}),
		fpcRender: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "shopanda_fpc_render_duration_seconds",
			Help:    "Full-page cache miss render duration in seconds, labelled by route template.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route"}),
		fpcPurge: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "shopanda_fpc_purge_total",
			Help: "Full-page cache keys deleted by purge, labelled by trigger (tag_invalidation/manual_url/unknown).",
		}, []string{"trigger"}),
		fpcBackendGet: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "shopanda_fpc_backend_get_errors_total",
			Help: "Full-page cache requests that observed a cache backend Get failure (at most one increment per request).",
		}),
	}
	reg.MustRegister(
		r.httpRequests,
		r.httpDuration,
		r.checkoutResult,
		r.jobFailures,
		r.webhookResult,
		r.rateLimitErr,
		r.fpcRequests,
		r.fpcRender,
		r.fpcPurge,
		r.fpcBackendGet,
	)
	return r, reg
}

func (r *PrometheusRecorder) HTTPRequest(routePattern, method, statusClass string, duration time.Duration) {
	r.httpRequests.WithLabelValues(routePattern, method, statusClass).Inc()
	r.httpDuration.WithLabelValues(routePattern, method).Observe(duration.Seconds())
}

func (r *PrometheusRecorder) CheckoutResult(outcome string) {
	r.checkoutResult.WithLabelValues(outcome).Inc()
}

func (r *PrometheusRecorder) JobFailure(jobType string) {
	r.jobFailures.WithLabelValues(jobType).Inc()
}

func (r *PrometheusRecorder) WebhookDelivery(outcome string) {
	r.webhookResult.WithLabelValues(outcome).Inc()
}

func (r *PrometheusRecorder) RateLimitBackendError(limiter, reason string) {
	if reason == "" {
		reason = "error"
	}
	r.rateLimitErr.WithLabelValues(limiter, reason).Inc()
}

func (r *PrometheusRecorder) FPCRequest(route, outcome string) {
	r.fpcRequests.WithLabelValues(route, outcome).Inc()
}

func (r *PrometheusRecorder) FPCRenderDuration(route string, duration time.Duration) {
	r.fpcRender.WithLabelValues(route).Observe(duration.Seconds())
}

func (r *PrometheusRecorder) FPCPurgeKeys(trigger string, keysDeleted int64) {
	if keysDeleted <= 0 {
		return
	}
	r.fpcPurge.WithLabelValues(trigger).Add(float64(keysDeleted))
}

func (r *PrometheusRecorder) FPCBackendGetError() {
	r.fpcBackendGet.Inc()
}

// Handler returns the Prometheus text-exposition scrape endpoint for reg.
func Handler(reg *prometheus.Registry) http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
}
