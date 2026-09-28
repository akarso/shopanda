package shared

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/ratelimit"
)

// RateLimiter is the narrow capability RateLimitMiddleware needs from a
// limiter backend — satisfied by *ratelimit.Limiter (memory, one bucket
// per process — the historical default) and by redis.RateLimiter (a
// shared sliding-window counter — PR-1041), selected via
// RateLimitConfig.Driver. Kept minimal and defined here, at the point of
// use, rather than in internal/platform/ratelimit: the interfaces layer
// must not import internal/infrastructure/redis directly (see AGENTS.md's
// dependency-direction rule), so the redis-backed implementation is
// constructed at the composition root (cmd/api) and handed in via
// LimiterFactory instead.
type RateLimiter interface {
	Allow(key string) bool
}

// LimiterFactory constructs a RateLimiter for one configured limit —
// scope is a stable name distinguishing which one ("default", or
// "route:"+PathPrefix for a per-route rule) so a Redis-backed factory can
// namespace keys per limit without different limits sharing a budget.
// A nil factory passed to RateLimitMiddleware defaults to the in-process
// token bucket, ignoring scope (each instance already has its own
// isolated bucket map, so no namespacing is needed there).
type LimiterFactory func(scope string, rate float64, burst int) (RateLimiter, error)

func memoryLimiterFactory(_ string, rate float64, burst int) (RateLimiter, error) {
	return ratelimit.NewLimiter(rate, burst), nil
}

// RateLimitMiddleware enforces per-IP rate limits via newLimiter (nil
// selects the in-process token bucket — RateLimitConfig.Driver="memory").
// It supports a default limiter for all routes and optional per-route
// limiters for path prefixes configured in RateLimitConfig.PerRoute.
// Returns 429 with Retry-After header when the limit is exceeded.
func RateLimitMiddleware(cfg config.RateLimitConfig, log logger.Logger, newLimiter LimiterFactory) (Middleware, error) {
	if !cfg.Enabled {
		return func(next http.Handler) http.Handler { return next }, nil
	}
	if newLimiter == nil {
		newLimiter = memoryLimiterFactory
	}

	trustedNets := ParseTrustedProxies(cfg.TrustedProxies)

	var defaultLimiter RateLimiter
	if cfg.Default.Rate > 0 && cfg.Default.Burst > 0 {
		lim, err := newLimiter("default", cfg.Default.Rate, cfg.Default.Burst)
		if err != nil {
			return nil, fmt.Errorf("rate limit: default limiter: %w", err)
		}
		defaultLimiter = lim
	}

	type routeLimiter struct {
		prefix  string
		limiter RateLimiter
	}
	var routeLimiters []routeLimiter
	for _, r := range cfg.PerRoute {
		if r.Rate > 0 && r.Burst > 0 && r.PathPrefix != "" {
			lim, err := newLimiter("route:"+r.PathPrefix, r.Rate, r.Burst)
			if err != nil {
				return nil, fmt.Errorf("rate limit: per-route limiter for %q: %w", r.PathPrefix, err)
			}
			routeLimiters = append(routeLimiters, routeLimiter{
				prefix:  r.PathPrefix,
				limiter: lim,
			})
		}
	}

	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := ClientIP(r, trustedNets)

			// Find the per-route limiter with the longest matching prefix.
			var matched RateLimiter
			matchLen := 0
			for _, rl := range routeLimiters {
				if strings.HasPrefix(r.URL.Path, rl.prefix) && len(rl.prefix) > matchLen {
					matched = rl.limiter
					matchLen = len(rl.prefix)
				}
			}
			if matched != nil {
				if !matched.Allow(ip) {
					log.Warn("ratelimit.rejected", map[string]interface{}{
						"client_ip": ip,
						"path":      r.URL.Path,
						"limiter":   "per_route",
					})
					WriteRateLimited(w)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			// Fall back to default limiter.
			if defaultLimiter != nil && !defaultLimiter.Allow(ip) {
				log.Warn("ratelimit.rejected", map[string]interface{}{
					"client_ip": ip,
					"path":      r.URL.Path,
					"limiter":   "default",
				})
				WriteRateLimited(w)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
	return mw, nil
}

// ParseTrustedProxies parses CIDR strings and bare IPs into a list of
// *net.IPNet used to decide whether to trust proxy headers.
func ParseTrustedProxies(proxies []string) []*net.IPNet {
	var nets []*net.IPNet
	for _, p := range proxies {
		if !strings.Contains(p, "/") {
			// Bare IP — wrap as /32 or /128.
			ip := net.ParseIP(strings.TrimSpace(p))
			if ip == nil {
				continue
			}
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		_, n, err := net.ParseCIDR(strings.TrimSpace(p))
		if err == nil {
			nets = append(nets, n)
		}
	}
	return nets
}

// isTrustedProxy reports whether peerIP falls within any trusted proxy network.
func isTrustedProxy(peerIP string, trusted []*net.IPNet) bool {
	ip := net.ParseIP(peerIP)
	if ip == nil {
		return false
	}
	for _, n := range trusted {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ClientIP extracts the client IP address. Proxy headers (X-Forwarded-For,
// X-Real-Ip) are only honoured when the immediate peer is a trusted proxy.
func ClientIP(r *http.Request, trusted []*net.IPNet) string {
	peer := peerIP(r)
	if len(trusted) > 0 && isTrustedProxy(peer, trusted) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if i := strings.IndexByte(xff, ','); i > 0 {
				return strings.TrimSpace(xff[:i])
			}
			return strings.TrimSpace(xff)
		}
		if xri := r.Header.Get("X-Real-Ip"); xri != "" {
			return strings.TrimSpace(xri)
		}
	}
	return peer
}

// peerIP extracts the IP of the direct connection peer from RemoteAddr.
func peerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// WriteRateLimited writes a 429 JSON error response with a Retry-After header.
func WriteRateLimited(w http.ResponseWriter) {
	w.Header().Set("Retry-After", strconv.FormatInt(int64(time.Second.Seconds()), 10))
	JSONError(w, apperror.RateLimited("rate limit exceeded"))
}
