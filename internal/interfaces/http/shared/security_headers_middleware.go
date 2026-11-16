package shared

import (
	"net/http"

	"github.com/akarso/shopanda/pkg/storefrontsession"
)

// Security header values (exact; covered by tests).
const (
	HeaderContentTypeOptions = "nosniff"
	HeaderFrameOptions       = "DENY"
	HeaderReferrerPolicy     = "strict-origin-when-cross-origin"
	HeaderHSTS               = "max-age=31536000; includeSubDomains"
)

// SecurityHeadersMiddleware sets browser security headers on every response.
//
// Always set:
//   - X-Content-Type-Options: nosniff
//   - X-Frame-Options: DENY (clickjacking protection for API + SSR; CSP frame-ancestors not used here)
//   - Referrer-Policy: strict-origin-when-cross-origin
//
// Strict-Transport-Security is set only when the request is TLS (r.TLS != nil)
// or when X-Forwarded-Proto: https is honored from a trusted proxy.
func SecurityHeadersMiddleware(trustedProxies ...string) Middleware {
	trusted := ParseTrustedProxies(trustedProxies)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", HeaderContentTypeOptions)
			h.Set("X-Frame-Options", HeaderFrameOptions)
			h.Set("Referrer-Policy", HeaderReferrerPolicy)
			secure := IsRequestSecure(r, trusted)
			if secure {
				h.Set("Strict-Transport-Security", HeaderHSTS)
			}
			// Propagate proxy-aware Secure for guest cart cookies (EnsureStorefrontCart).
			next.ServeHTTP(w, r.WithContext(storefrontsession.WithCookieSecure(r.Context(), secure)))
		})
	}
}
