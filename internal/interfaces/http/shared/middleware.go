package shared

import (
	"fmt"
	"net/http"
	"time"

	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/requestctx"
)

// RequestIDMiddleware wraps requestctx.Middleware as a router Middleware.
func RequestIDMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return requestctx.Middleware(next)
	}
}

// LoggingMiddleware logs each request with method, path, status, and duration.
func LoggingMiddleware(log logger.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := wrapStatus(w)
			next.ServeHTTP(sw, r)
			log.Info("http.request", map[string]interface{}{
				"method":      r.Method,
				"path":        r.URL.Path,
				"status":      sw.status,
				"duration_ms": time.Since(start).Milliseconds(),
				"request_id":  requestctx.RequestID(r.Context()),
			})
		})
	}
}

// RecoveryMiddleware catches panics and returns 500.
func RecoveryMiddleware(log logger.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error("http.panic", fmt.Errorf("%v", rec), map[string]interface{}{
						"method":     r.Method,
						"path":       r.URL.Path,
						"request_id": requestctx.RequestID(r.Context()),
					})
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(`{"data":null,"error":{"code":"internal","message":"internal server error"}}`))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// statusWriter wraps http.ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

// wrapStatus returns w as a *statusWriter, reusing one already present
// from an outer middleware in the same chain instead of allocating and
// layering a new wrapper around it. Metrics, Tracing, and Logging
// middleware each only need the response's final status code — when more
// than one of them wraps the same request (the normal case: Metrics and
// Tracing are both outermost, Logging sits further in), each inner call
// would otherwise allocate its own wrapper around the outer one's,
// tripling the allocations for the exact same piece of information.
func wrapStatus(w http.ResponseWriter) *statusWriter {
	if sw, ok := w.(*statusWriter); ok {
		return sw
	}
	return &statusWriter{ResponseWriter: w, status: http.StatusOK}
}

// Unwrap exposes the underlying ResponseWriter for http.MaxBytesReader /
// ResponseController compatibility through middleware chains.
func (sw *statusWriter) Unwrap() http.ResponseWriter {
	return sw.ResponseWriter
}
