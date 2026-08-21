package metrics_test

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/platform/metrics"
)

func TestNoop_DoesNotPanic(t *testing.T) {
	rec := metrics.Noop()
	rec.HTTPRequest("GET /api/v1/products/{id}", "GET", "2xx", 10*time.Millisecond)
	rec.CheckoutResult(metrics.OutcomeSuccess)
	rec.JobFailure("webhook.deliver")
	rec.WebhookDelivery(metrics.OutcomeFailed)
}

func TestPrometheusRecorder_ExposesExpectedMetrics(t *testing.T) {
	rec, reg := metrics.NewPrometheusRecorder()

	rec.HTTPRequest("GET /api/v1/products/{id}", "GET", "2xx", 25*time.Millisecond)
	rec.HTTPRequest("GET /api/v1/products/{id}", "GET", "5xx", 5*time.Millisecond)
	rec.CheckoutResult(metrics.OutcomeSuccess)
	rec.CheckoutResult(metrics.OutcomeFailed)
	rec.JobFailure("webhook.deliver")
	rec.WebhookDelivery(metrics.OutcomeSuccess)

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	metrics.Handler(reg).ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()

	for _, want := range []string{
		`shopanda_http_requests_total{method="GET",route="GET /api/v1/products/{id}",status_class="2xx"} 1`,
		`shopanda_http_requests_total{method="GET",route="GET /api/v1/products/{id}",status_class="5xx"} 1`,
		`shopanda_checkout_result_total{outcome="success"} 1`,
		`shopanda_checkout_result_total{outcome="failed"} 1`,
		`shopanda_job_failures_total{job_type="webhook.deliver"} 1`,
		`shopanda_webhook_deliveries_total{outcome="success"} 1`,
		"shopanda_http_request_duration_seconds",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected metrics output to contain %q, got:\n%s", want, body)
		}
	}
}

// TestPrometheusRecorder_BoundedLabelCardinality documents the bounded-label
// contract: the same finite set of label values, called repeatedly, must
// only ever produce one time series per combination — never one per call.
func TestPrometheusRecorder_BoundedLabelCardinality(t *testing.T) {
	rec, reg := metrics.NewPrometheusRecorder()
	for i := 0; i < 50; i++ {
		rec.HTTPRequest("GET /api/v1/products/{id}", "GET", "2xx", time.Millisecond)
	}

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	metrics.Handler(reg).ServeHTTP(w, req)
	body := w.Body.String()

	want := `shopanda_http_requests_total{method="GET",route="GET /api/v1/products/{id}",status_class="2xx"} 50`
	if !strings.Contains(body, want) {
		t.Errorf("expected a single accumulating series %q, got:\n%s", want, body)
	}
	if strings.Count(body, "shopanda_http_requests_total{") != 1 {
		t.Errorf("expected exactly one shopanda_http_requests_total series, got body:\n%s", body)
	}
}
