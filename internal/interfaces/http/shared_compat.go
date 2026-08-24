package http

// Compatibility layer for PR-1021 (HTTP package split, shared primitives).
//
// The router, middleware, response envelope, pagination, and server
// primitives now live in internal/interfaces/http/shared so that future
// handler packages (PR-1022 admin, PR-1023 storefront) can depend on them
// without importing this package (which would otherwise risk import
// cycles once handlers move out of package http).
//
// This file re-exports the same names from this package so none of the
// ~170 existing handler/test files in this package need to change in this
// PR. New code should prefer importing the shared package directly; this
// shim is expected to shrink (and eventually disappear) as handlers move
// into their own packages in PR-1022/1023.

import (
	"net"
	"net/http"

	"github.com/akarso/shopanda/internal/domain/routing"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/metrics"
)

// --- Router ---

type Router = shared.Router
type Middleware = shared.Middleware

func NewRouter() *Router { return shared.NewRouter() }

// --- Generic middleware ---

func RequestIDMiddleware() Middleware { return shared.RequestIDMiddleware() }

func LoggingMiddleware(log logger.Logger) Middleware { return shared.LoggingMiddleware(log) }

func RecoveryMiddleware(log logger.Logger) Middleware { return shared.RecoveryMiddleware(log) }

// --- Response envelope ---

type Response = shared.Response
type ErrorBody = shared.ErrorBody

func JSON(w http.ResponseWriter, status int, data interface{}) { shared.JSON(w, status, data) }

func JSONWithError(w http.ResponseWriter, status int, data interface{}, err error) {
	shared.JSONWithError(w, status, data, err)
}

func JSONError(w http.ResponseWriter, err error) { shared.JSONError(w, err) }

func StatusFromCode(code apperror.Code) int { return shared.StatusFromCode(code) }

// --- Pagination ---

func ParsePagination(r *http.Request) (offset, limit int, err error) {
	return shared.ParsePagination(r)
}

// --- Server ---

type Server = shared.Server

func NewServer(host string, port int, handler http.Handler, log logger.Logger) *Server {
	return shared.NewServer(host, port, handler, log)
}

// --- Security headers ---

const (
	HeaderContentTypeOptions = shared.HeaderContentTypeOptions
	HeaderFrameOptions       = shared.HeaderFrameOptions
	HeaderReferrerPolicy     = shared.HeaderReferrerPolicy
	HeaderHSTS               = shared.HeaderHSTS
)

func SecurityHeadersMiddleware(trustedProxies ...string) Middleware {
	return shared.SecurityHeadersMiddleware(trustedProxies...)
}

// --- Body limit ---

func BodyLimitMiddleware(defaultLimit, mediaLimit int64) Middleware {
	return shared.BodyLimitMiddleware(defaultLimit, mediaLimit)
}

// --- Cache control ---

func CacheControlMiddleware(noCachePrefixes []string) Middleware {
	return shared.CacheControlMiddleware(noCachePrefixes)
}

// --- Rate limiting ---

func RateLimitMiddleware(cfg config.RateLimitConfig, log logger.Logger) Middleware {
	return shared.RateLimitMiddleware(cfg, log)
}

// parseTrustedProxies, clientIP, writeRateLimited are used directly by
// handler files in this package (health.go, storefront.go, auth.go) that
// implement their own rate limiting / IP resolution outside the
// RateLimitMiddleware chain.
func parseTrustedProxies(proxies []string) []*net.IPNet { return shared.ParseTrustedProxies(proxies) }

func clientIP(r *http.Request, trusted []*net.IPNet) string { return shared.ClientIP(r, trusted) }

func writeRateLimited(w http.ResponseWriter) { shared.WriteRateLimited(w) }

// --- CSRF ---

func CSRFMiddleware(trustedProxies ...string) Middleware {
	return shared.CSRFMiddleware(trustedProxies...)
}

// shopandaCSRFToken is used directly by storefront handler files that read
// the CSRF token for form rendering, outside the CSRFMiddleware chain.
func shopandaCSRFToken(r *http.Request) string { return shared.CSRFToken(r) }

// isRequestSecure is used directly by storefront handler files that set
// cookies (Secure flag) outside the CSRFMiddleware/SecurityHeadersMiddleware chain.
func isRequestSecure(r *http.Request, trusted []*net.IPNet) bool {
	return shared.IsRequestSecure(r, trusted)
}

// --- Language ---

func LanguageMiddleware() Middleware { return shared.LanguageMiddleware() }

// --- Store resolution ---

func StoreMiddleware(repo store.StoreRepository, log logger.Logger) Middleware {
	return shared.StoreMiddleware(repo, log)
}

// --- URL rewrite resolution ---

func ResolverMiddleware(repo routing.RewriteRepository, log logger.Logger) Middleware {
	return shared.ResolverMiddleware(repo, log)
}

// --- Metrics ---

func MetricsMiddleware(rec metrics.Recorder) Middleware {
	return shared.MetricsMiddleware(rec)
}
