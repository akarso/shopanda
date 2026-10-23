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

// Outcome labels are a fixed, small enum, bounded by construction.
const (
	OutcomeSuccess = "success"
	OutcomeFailed  = "failed"

	// OutcomeSucceededEventFailed is for CheckoutResult only: every checkout
	// step succeeded (order created, payment captured, etc.) but the final
	// EventCheckoutCompleted publish failed. Recording that as OutcomeFailed
	// would conflate a real checkout failure with a downstream
	// notification/event-bus problem on an otherwise-successful order.
	OutcomeSucceededEventFailed = "succeeded_event_failed"

	// Full-page cache outcomes (PR-1047). Bounded enum — never raw paths.
	// Terminal dispositions only: hit / miss / bypass. Backend Get failures
	// use FPCBackendGetError, not an outcome label.
	FPCOutcomeHit    = "hit"
	FPCOutcomeMiss   = "miss"
	FPCOutcomeBypass = "bypass"

	// Full-page cache purge triggers (PR-1047). Values count keys deleted,
	// not soft-TTL observations. Unknown triggers use FPCPurgeUnknown.
	FPCPurgeTagInvalidation = "tag_invalidation"
	FPCPurgeManualURL       = "manual_url"
	FPCPurgeUnknown         = "unknown"
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
	// be OutcomeSuccess, OutcomeFailed, or OutcomeSucceededEventFailed.
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

	// RateLimitBackendError records one Redis (or other remote) limiter
	// failure. limiter is the configured name ("default" or "route:<prefix>"),
	// never a client IP. reason is a bounded enum: "error", "circuit_open",
	// or "pool_timeout".
	RateLimitBackendError(limiter, reason string)

	// FPCRequest records one full-page-cache decision. route is an FPC
	// allowlist template (e.g. "/products/{slug}"), never a raw path.
	// outcome is FPCOutcomeHit, FPCOutcomeMiss, or FPCOutcomeBypass.
	FPCRequest(route, outcome string)

	// FPCRenderDuration records miss-path render time for an FPC route
	// template (what a hit avoids).
	FPCRenderDuration(route string, duration time.Duration)

	// FPCPurgeKeys records keys removed by a purge. trigger is
	// FPCPurgeTagInvalidation, FPCPurgeManualURL, or FPCPurgeUnknown —
	// never a tag name or URL. keysDeleted must be > 0.
	FPCPurgeKeys(trigger string, keysDeleted int64)

	// FPCBackendGetError records one FPC request that observed a cache
	// backend Get failure (at most once per request at the call site).
	// No labels — bounded by construction.
	FPCBackendGetError()
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
func (noopRecorder) RateLimitBackendError(string, string)              {}
func (noopRecorder) FPCRequest(string, string)                         {}
func (noopRecorder) FPCRenderDuration(string, time.Duration)           {}
func (noopRecorder) FPCPurgeKeys(string, int64)                        {}
func (noopRecorder) FPCBackendGetError()                               {}
