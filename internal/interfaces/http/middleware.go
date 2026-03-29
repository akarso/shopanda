package http

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
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
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
