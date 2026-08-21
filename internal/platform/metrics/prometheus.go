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
	}
	reg.MustRegister(
		r.httpRequests,
		r.httpDuration,
		r.checkoutResult,
		r.jobFailures,
		r.webhookResult,
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

// Handler returns the Prometheus text-exposition scrape endpoint for reg.
func Handler(reg *prometheus.Registry) http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
}
