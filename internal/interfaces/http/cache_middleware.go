package http

import (
	"net/http"
	"strings"
)

// CacheControlMiddleware sets Cache-Control response headers based on the
// request path. GET requests to cacheable paths receive "public, max-age=300";
// all other requests receive "no-store". Write methods (POST, PUT, DELETE, PATCH)
// always get "no-store".
//
// noCachePrefixes lists path prefixes that must never be cached (e.g.
// "/api/v1/carts", "/api/v1/checkout", "/api/v1/account", "/api/v1/orders",
// "/api/v1/auth").
func CacheControlMiddleware(noCachePrefixes []string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer next.ServeHTTP(w, r)

			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Cache-Control", "no-store")
				return
			}

			for _, prefix := range noCachePrefixes {
				if strings.HasPrefix(r.URL.Path, prefix) {
					w.Header().Set("Cache-Control", "no-store")
					return
				}
			}

			w.Header().Set("Cache-Control", "public, max-age=300")
		})
	}
}
