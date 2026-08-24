package shared

import (
	"net/http"
	"time"

	"github.com/akarso/shopanda/internal/platform/metrics"
)

// unmatchedRoute is the bounded label used for requests that don't match any
// registered pattern (404s, mux-internal redirects) — never the raw path.
const unmatchedRoute = "unmatched"

// methodLabels is the fixed set of HTTP methods this API's routes use.
var methodLabels = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodPost:    true,
	http.MethodPut:     true,
	http.MethodPatch:   true,
	http.MethodDelete:  true,
	http.MethodOptions: true,
}

// methodLabel buckets the request method into the fixed set above, or
// "other" — never the raw, attacker-controlled method string. The HTTP
// method is technically an arbitrary token (RFC 7230), so passing it
// through unchecked is the same unbounded-cardinality risk statusClass
// guards against for status codes.
func methodLabel(method string) string {
	if methodLabels[method] {
		return method
	}
	return "other"
}

// statusClass buckets an HTTP status code into a fixed, bounded label —
// "2xx".."5xx", or "other" — never the raw numeric status code.
func statusClass(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500 && code < 600:
		return "5xx"
	default:
		return "other"
	}
}

// MetricsMiddleware records RED (rate/errors/duration) metrics for every
// request using bounded labels only: the matched route template (not the
// raw URL), the HTTP method, and the status class (not the raw status
// code). Register this as the outermost middleware (before Recovery) so it
// captures the final response status and total request duration.
//
// The route template is read from the *routeMatch box captured during
// dispatch (see router.go's captureRoute) rather than by independently
// re-resolving the pattern here — the latter would run net/http.ServeMux's
// matching twice per request, once for the label and once for real dispatch.
func MetricsMiddleware(rec metrics.Recorder) Middleware {
	if rec == nil {
		rec = metrics.Noop()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			pattern := routeMatchFromContext(r.Context())
			if pattern == "" {
				pattern = unmatchedRoute
			}
			rec.HTTPRequest(pattern, methodLabel(r.Method), statusClass(sw.status), time.Since(start))
		})
	}
}
