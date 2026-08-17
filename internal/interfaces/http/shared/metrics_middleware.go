package shared

import (
	"net/http"
	"time"

	"github.com/akarso/shopanda/internal/platform/metrics"
)

// unmatchedRoute is the bounded label used for requests that don't match any
// registered pattern (404s, mux-internal redirects) — never the raw path.
const unmatchedRoute = "unmatched"

// RoutePatternResolver resolves the registered route template for a request
// without invoking its handler. *Router implements this.
type RoutePatternResolver interface {
	RoutePattern(req *http.Request) string
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
func MetricsMiddleware(rec metrics.Recorder, routes RoutePatternResolver) Middleware {
	if rec == nil {
		rec = metrics.Noop()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			pattern := unmatchedRoute
			if routes != nil {
				if p := routes.RoutePattern(r); p != "" {
					pattern = p
				}
			}
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			rec.HTTPRequest(pattern, r.Method, statusClass(sw.status), time.Since(start))
		})
	}
}
