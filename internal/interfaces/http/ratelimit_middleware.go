package http

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/ratelimit"
)

// RateLimitMiddleware enforces per-IP token-bucket rate limits.
// It supports a default limiter for all routes and optional per-route
// limiters for path prefixes configured in RateLimitConfig.PerRoute.
// Returns 429 with Retry-After header when the limit is exceeded.
func RateLimitMiddleware(cfg config.RateLimitConfig) Middleware {
	if !cfg.Enabled {
		return func(next http.Handler) http.Handler { return next }
	}

	var defaultLimiter *ratelimit.Limiter
	if cfg.Default.Rate > 0 && cfg.Default.Burst > 0 {
		defaultLimiter = ratelimit.NewLimiter(cfg.Default.Rate, cfg.Default.Burst)
	}

	type routeLimiter struct {
		prefix  string
		limiter *ratelimit.Limiter
	}
	var routeLimiters []routeLimiter
	for _, r := range cfg.PerRoute {
		if r.Rate > 0 && r.Burst > 0 && r.PathPrefix != "" {
			routeLimiters = append(routeLimiters, routeLimiter{
				prefix:  r.PathPrefix,
				limiter: ratelimit.NewLimiter(r.Rate, r.Burst),
			})
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)

			// Check per-route limiter first (most specific match).
			for _, rl := range routeLimiters {
				if strings.HasPrefix(r.URL.Path, rl.prefix) {
					if !rl.limiter.Allow(ip) {
						writeRateLimited(w)
						return
					}
					// Per-route limiter matched and allowed; skip default.
					next.ServeHTTP(w, r)
					return
				}
			}

			// Fall back to default limiter.
			if defaultLimiter != nil && !defaultLimiter.Allow(ip) {
				writeRateLimited(w)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// clientIP extracts the client IP from X-Forwarded-For or RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// First entry is the original client.
		if i := strings.IndexByte(xff, ','); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if xff := r.Header.Get("X-Real-Ip"); xff != "" {
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeRateLimited(w http.ResponseWriter) {
	w.Header().Set("Retry-After", strconv.FormatInt(int64(time.Second.Seconds()), 10))
	JSONError(w, apperror.RateLimited("rate limit exceeded"))
}
