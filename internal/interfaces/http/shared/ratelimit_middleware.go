package shared

import (
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/ratelimit"
)

// statusClientClosed is nginx's 499 (client closed request). Used when the
// limiter sees an already-cancelled context so HTTP metrics record 4xx
// rather than the default 2xx. The client is gone; the body is unused.
const statusClientClosed = 499

// RateLimitMiddleware enforces per-IP rate limits with the in-process
// token-bucket backend. Prefer RateLimitMiddlewareWithFactory when the
// serve path has resolved rate_limit.driver.
func RateLimitMiddleware(cfg config.RateLimitConfig, log logger.Logger) Middleware {
	return RateLimitMiddlewareWithFactory(cfg, log, nil)
}

// RateLimitMiddlewareWithFactory is RateLimitMiddleware with an injectable
// backend factory. A nil factory uses the in-process token bucket. It
// supports a default limiter for all routes and optional per-route
// limiters for path prefixes configured in RateLimitConfig.PerRoute.
// Returns 429 with Retry-After header when the limit is exceeded.
func RateLimitMiddlewareWithFactory(cfg config.RateLimitConfig, log logger.Logger, factory ratelimit.Factory) Middleware {
	if !cfg.Enabled {
		return func(next http.Handler) http.Handler { return next }
	}
	if factory == nil {
		factory = ratelimit.MemoryFactory()
	}

	trustedNets := ParseTrustedProxies(cfg.TrustedProxies)
	backend := cfg.Driver
	if backend == "" {
		backend = "memory"
	}

	var defaultLimiter ratelimit.Allow
	if cfg.Default.Rate > 0 && cfg.Default.Burst > 0 {
		defaultLimiter = factory.New(ratelimit.Spec{
			Name:    "default",
			Rate:    cfg.Default.Rate,
			Burst:   cfg.Default.Burst,
			OnError: cfg.OnError,
		})
	}

	type routeLimiter struct {
		prefix  string
		limiter ratelimit.Allow
	}
	var routeLimiters []routeLimiter
	for _, r := range cfg.PerRoute {
		if r.Rate > 0 && r.Burst > 0 && r.PathPrefix != "" {
			onErr := r.OnError
			if onErr == "" {
				onErr = cfg.OnError
			}
			routeLimiters = append(routeLimiters, routeLimiter{
				prefix: r.PathPrefix,
				limiter: factory.New(ratelimit.Spec{
					Name:    "route:" + r.PathPrefix,
					Rate:    r.Rate,
					Burst:   r.Burst,
					OnError: onErr,
				}),
			})
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := ClientIP(r, trustedNets)

			// Find the per-route limiter with the longest matching prefix.
			var matched ratelimit.Allow
			matchLen := 0
			limiterName := "default"
			for _, rl := range routeLimiters {
				if strings.HasPrefix(r.URL.Path, rl.prefix) && len(rl.prefix) > matchLen {
					matched = rl.limiter
					matchLen = len(rl.prefix)
					limiterName = "per_route"
				}
			}
			if matched != nil {
				d := matched.Allow(r.Context(), ip)
				if finishLimiter(w, log, d, ip, r.URL.Path, limiterName, backend) {
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			// Fall back to default limiter.
			if defaultLimiter != nil {
				d := defaultLimiter.Allow(r.Context(), ip)
				if finishLimiter(w, log, d, ip, r.URL.Path, "default", backend) {
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
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

// ClientIP extracts the client IP address. Proxy headers are only honoured
// when the immediate peer is a trusted proxy. All X-Forwarded-For lines are
// joined (proxies such as HAProxy option forwardfor add a second line
// instead of appending) and walked from the right, skipping trusted hops.
// The first invalid entry stops the walk and falls back to the peer — never
// to X-Real-Ip (the client can set that when the proxy only adds XFF).
// X-Real-Ip is used only when X-Forwarded-For is absent. The result is
// net.ParseIP-canonicalized.
func ClientIP(r *http.Request, trusted []*net.IPNet) string {
	peer := canonicalizeIP(peerIP(r))
	if peer == "" {
		peer = peerIP(r)
	}
	if len(trusted) == 0 || !isTrustedProxy(peer, trusted) {
		return peer
	}
	if lines := r.Header.Values("X-Forwarded-For"); len(lines) > 0 {
		if ip, invalid := clientIPFromXFF(strings.Join(lines, ","), trusted); invalid || ip == "" {
			return peer
		} else {
			return ip
		}
	}
	if realIP := canonicalizeIP(r.Header.Get("X-Real-Ip")); realIP != "" {
		return realIP
	}
	return peer
}

func canonicalizeIP(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if ip := net.ParseIP(s); ip != nil {
		return ip.String()
	}
	host, _, err := net.SplitHostPort(s)
	if err != nil {
		return ""
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return ""
	}
	return ip.String()
}

func clientIPFromXFF(xff string, trusted []*net.IPNet) (ip string, invalid bool) {
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		raw := strings.TrimSpace(parts[i])
		if raw == "" {
			continue
		}
		parsed := canonicalizeIP(raw)
		if parsed == "" {
			return "", true
		}
		if isTrustedProxy(parsed, trusted) {
			continue
		}
		return parsed, false
	}
	return "", false
}

func finishLimiter(w http.ResponseWriter, log logger.Logger, d ratelimit.Decision, ip, path, limiter, backend string) bool {
	if d.Canceled {
		// nginx 499: client closed the request. Nobody will read it, but
		// MetricsMiddleware would otherwise record the default 2xx.
		w.WriteHeader(statusClientClosed)
		return true
	}
	if !d.Allowed {
		logReject(log, d, ip, path, limiter, backend)
		WriteRateLimitedAfter(w, ratelimit.RetryAfterSeconds(d.RetryAfter))
		return true
	}
	return false
}

func logReject(log logger.Logger, d ratelimit.Decision, ip, path, limiter, backend string) {
	fields := map[string]interface{}{
		"path":    path,
		"limiter": limiter,
		"backend": backend,
	}
	if d.BackendError {
		fields["backend_error"] = true
	}
	// Fail-closed outage 429s omit client_ip (PII, high cardinality).
	// Local-fallback denials keep it so an abuser is still traceable.
	if !d.BackendError || d.LocalFallback {
		fields["client_ip"] = ip
	}
	log.Warn("ratelimit.rejected", fields)
}

// peerIP extracts the IP of the direct connection peer from RemoteAddr.
func peerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// WriteRateLimited writes a 429 JSON error response with Retry-After: 1.
func WriteRateLimited(w http.ResponseWriter) {
	WriteRateLimitedAfter(w, 1)
}

// WriteRateLimitedAfter is WriteRateLimited with an explicit Retry-After.
func WriteRateLimitedAfter(w http.ResponseWriter, seconds int) {
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	JSONError(w, apperror.RateLimited("rate limit exceeded"))
}
