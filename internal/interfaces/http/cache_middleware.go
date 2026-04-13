package http

import (
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/platform/auth"
)

// CacheControlMiddleware sets Cache-Control response headers based on the
// request path and authentication state. GET/HEAD requests to cacheable paths
// receive "public, max-age=300"; all other requests receive "no-store".
// Write methods (POST, PUT, DELETE, PATCH) always get "no-store".
// Authenticated (non-guest) requests always get "no-store".
//
// noCachePrefixes lists path prefixes that must never be cached (e.g.
// "/api/v1/carts", "/api/v1/checkout", "/api/v1/account", "/api/v1/orders",
// "/api/v1/auth"). A prefix matches when the path equals it exactly or
// continues with a '/' separator.
func CacheControlMiddleware(noCachePrefixes []string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer next.ServeHTTP(w, r)

			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Cache-Control", "no-store")
				return
			}

			path := r.URL.Path
			for _, prefix := range noCachePrefixes {
				if path == prefix || strings.HasPrefix(path, prefix+"/") {
					w.Header().Set("Cache-Control", "no-store")
					return
				}
			}

			if id := auth.IdentityFrom(r.Context()); !id.IsGuest() {
				w.Header().Set("Cache-Control", "no-store")
				return
			}

			w.Header().Set("Cache-Control", "public, max-age=300")
		})
	}
}
