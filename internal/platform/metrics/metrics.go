// Package metrics defines the cross-cutting Recorder port used to observe
// RED (rate/errors/duration) and business outcomes across the platform.
//
// All label values recorded through this package must be bounded — a
// finite, small set of known strings (route templates, HTTP methods, status
// classes, job types, outcomes). Never pass raw URLs, query strings, user
// IDs, order IDs, webhook destination URLs, or emails as label values: each
// distinct label combination becomes a permanent Prometheus time series, and
// unbounded labels cause unbounded memory growth (cardinality explosion).
package metrics

import "time"

// Outcome labels are a fixed two-value enum, bounded by construction.
const (
	OutcomeSuccess = "success"
	OutcomeFailed  = "failed"
)

// Recorder records RED and business metrics. Implementations must be safe
// for concurrent use by multiple goroutines.
type Recorder interface {
	// HTTPRequest records one completed HTTP request. routePattern must be
	// the matched route template (e.g. "GET /api/v1/products/{id}"), never
	// the raw URL path. statusClass must be one of "2xx", "3xx", "4xx",
	// "5xx", or "other" — never the raw numeric status code.
	HTTPRequest(routePattern, method, statusClass string, duration time.Duration)

	// CheckoutResult records one completed checkout attempt. outcome must
	// be OutcomeSuccess or OutcomeFailed.
	CheckoutResult(outcome string)

	// JobFailure records one failed background job execution. jobType must
	// be the job's registered type string (a fixed, compile-time set —
	// never a job ID or payload value).
	JobFailure(jobType string)

	// WebhookDelivery records one attempted outbound webhook delivery.
	// outcome must be OutcomeSuccess or OutcomeFailed. Skipped deliveries
	// (inactive/unsubscribed endpoints) are not delivery attempts and must
	// not be recorded.
	WebhookDelivery(outcome string)
}

// noopRecorder discards every recording. Used when metrics are disabled so
// call sites never need a nil check.
type noopRecorder struct{}

// Noop returns a Recorder that discards all recordings.
func Noop() Recorder { return noopRecorder{} }

func (noopRecorder) HTTPRequest(string, string, string, time.Duration) {}
func (noopRecorder) CheckoutResult(string)                             {}
func (noopRecorder) JobFailure(string)                                 {}
func (noopRecorder) WebhookDelivery(string)                            {}
