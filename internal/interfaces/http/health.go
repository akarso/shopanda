package http

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/ratelimit"
)

// HealthResponse is the JSON body returned by the health and readiness endpoints.
type HealthResponse struct {
	Status string `json:"status"`
}

func setProbeCacheHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

// HealthHandler returns an http.HandlerFunc for GET/HEAD /healthz (process liveness).
func HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setProbeCacheHeaders(w)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodHead {
			return
		}
		_ = json.NewEncoder(w).Encode(HealthResponse{Status: "ok"})
	}
}

// DBPinger is the subset of database/sql.DB used by readiness checks.
type DBPinger interface {
	PingContext(ctx context.Context) error
}

// DefaultReadyTimeout bounds DB ping wait for GET /readyz.
const DefaultReadyTimeout = 2 * time.Second

// Default ready-probe token bucket (matches historical default HTTP rate limit).
const (
	DefaultReadyProbeRate  = 10.0
	DefaultReadyProbeBurst = 20
)

// ReadyHandler returns GET/HEAD /readyz — 200 if the DB pings within DefaultReadyTimeout, else 503.
// Callers must not pass a typed-nil DBPinger (e.g. (*sql.DB)(nil)); only a nil interface is treated
// as unavailable. Production wiring uses db.Open, which returns a non-nil *sql.DB on success.
func ReadyHandler(db DBPinger) http.HandlerFunc {
	return ReadyHandlerWithTimeout(db, DefaultReadyTimeout)
}

// ReadyHandlerWithTimeout is like ReadyHandler with an explicit ping timeout.
// See ReadyHandler for the nil-DBPinger contract.
func ReadyHandlerWithTimeout(db DBPinger, timeout time.Duration) http.HandlerFunc {
	if timeout <= 0 {
		timeout = DefaultReadyTimeout
	}
	return func(w http.ResponseWriter, r *http.Request) {
		setProbeCacheHeaders(w)
		w.Header().Set("Content-Type", "application/json")
		writeReady := func(code int, status string) {
			w.WriteHeader(code)
			if r.Method == http.MethodHead {
				return
			}
			_ = json.NewEncoder(w).Encode(HealthResponse{Status: status})
		}
		if db == nil {
			writeReady(http.StatusServiceUnavailable, "unavailable")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			writeReady(http.StatusServiceUnavailable, "unavailable")
			return
		}
		writeReady(http.StatusOK, "ok")
	}
}

// ReadyProbeLimitMiddleware bounds /readyz DB pings per client IP.
// Probes mount outside RateLimitMiddleware; without this, an internet-reachable
// /readyz can flood PingContext and starve the shared DB pool.
// rate/burst <= 0 fall back to DefaultReadyProbeRate/Burst. Always enabled.
func ReadyProbeLimitMiddleware(trustedProxies []string, rate float64, burst int, log logger.Logger) Middleware {
	if rate <= 0 {
		rate = DefaultReadyProbeRate
	}
	if burst <= 0 {
		burst = DefaultReadyProbeBurst
	}
	limiter := ratelimit.NewLimiter(rate, burst)
	trusted := parseTrustedProxies(trustedProxies)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r, trusted)
			if !limiter.Allow(ip) {
				if log != nil {
					log.Warn("ratelimit.rejected", map[string]interface{}{
						"client_ip": ip,
						"path":      r.URL.Path,
						"limiter":   "ready_probe",
					})
				}
				setProbeCacheHeaders(w)
				writeRateLimited(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// MountProbes serves GET/HEAD /healthz and /readyz before next, so probes skip
// store/auth middleware that may touch the database or hang.
// Apply Recovery/SecurityHeaders/RequestID/Logging (and ReadyProbeLimitMiddleware for /readyz)
// to the individual probe handlers when wiring — not to the returned root.
func MountProbes(health, ready http.Handler, next http.Handler) http.Handler {
	if health == nil {
		health = HealthHandler()
	}
	if ready == nil {
		ready = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			setProbeCacheHeaders(w)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			if r.Method == http.MethodHead {
				return
			}
			_ = json.NewEncoder(w).Encode(HealthResponse{Status: "unavailable"})
		})
	}
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			switch r.URL.Path {
			case "/healthz":
				health.ServeHTTP(w, r)
				return
			case "/readyz":
				ready.ServeHTTP(w, r)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
