package http

import (
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

// RateLimitMiddleware enforces per-IP token-bucket rate limits.
// It supports a default limiter for all routes and optional per-route
// limiters for path prefixes configured in RateLimitConfig.PerRoute.
// Returns 429 with Retry-After header when the limit is exceeded.
func RateLimitMiddleware(cfg config.RateLimitConfig, log logger.Logger) Middleware {
	if !cfg.Enabled {
		return func(next http.Handler) http.Handler { return next }
	}

	trustedNets := parseTrustedProxies(cfg.TrustedProxies)

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
			ip := clientIP(r, trustedNets)

			// Find the per-route limiter with the longest matching prefix.
			var matched *ratelimit.Limiter
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
					writeRateLimited(w)
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
				writeRateLimited(w)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// parseTrustedProxies parses CIDR strings and bare IPs into a list of
// *net.IPNet used to decide whether to trust proxy headers.
func parseTrustedProxies(proxies []string) []*net.IPNet {
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

// clientIP extracts the client IP address. Proxy headers (X-Forwarded-For,
// X-Real-Ip) are only honoured when the immediate peer is a trusted proxy.
func clientIP(r *http.Request, trusted []*net.IPNet) string {
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

func writeRateLimited(w http.ResponseWriter) {
	w.Header().Set("Retry-After", strconv.FormatInt(int64(time.Second.Seconds()), 10))
	JSONError(w, apperror.RateLimited("rate limit exceeded"))
}
