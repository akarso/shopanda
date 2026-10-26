package shared

import (
	"net/http"
	"strings"
)

// storefrontCORSPrefixes are the only paths that receive CORS headers.
// Admin, setup, webhooks, and docs stay same-origin.
var storefrontCORSPrefixes = []string{
	"/api/v1/products",
	"/api/v1/categories",
	"/api/v1/search",
	"/api/v1/pages",
	"/api/v1/menus",
	"/api/v1/content-blocks",
	"/api/v1/carts",
	"/api/v1/checkout",
	"/api/v1/orders",
	"/api/v1/account",
	"/api/v1/auth",
	"/api/v1/shipping",
}

// CORSMiddleware adds CORS headers for storefront API paths when Origin is allowed.
//
// An empty allowlist is a no-op. "*" is anonymous only: Access-Control-Allow-Origin
// is literally "*" and credentials are never enabled. allowCredentials requires
// an explicit origin list (no wildcard) so a sibling host cannot ride shopper cookies.
func CORSMiddleware(allowedOrigins []string, allowCredentials bool) Middleware {
	allowAnonAll := false
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		if origin == "*" {
			allowAnonAll = true
			continue
		}
		allowed[origin] = struct{}{}
	}
	if allowAnonAll && allowCredentials {
		// Wildcard + credentials is the classic misconfig. Ignore "*".
		allowAnonAll = false
	}
	if !allowAnonAll && len(allowed) == 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !corsPathAllowed(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if len(allowed) > 0 {
				// Explicit allowlists vary by Origin even when the request
				// has none or an unlisted one, so caches cannot reuse a
				// credentialed/reflected response across origins.
				w.Header().Add("Vary", "Origin")
			}
			matchedExplicit := origin != "" && originAllowed(allowed, origin)
			if matchedExplicit {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, X-Request-ID, X-Shopanda-Field-Names")
				if allowCredentials {
					h.Set("Access-Control-Allow-Credentials", "true")
				}
				h.Set("Access-Control-Max-Age", "600")
			} else if allowAnonAll && origin != "" {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", "*")
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, X-Request-ID, X-Shopanda-Field-Names")
				h.Set("Access-Control-Max-Age", "600")
			} else if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			if r.Method == http.MethodOptions && (matchedExplicit || allowAnonAll) {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func corsPathAllowed(path string) bool {
	if strings.HasPrefix(path, "/api/v1/admin") {
		return false
	}
	for _, prefix := range storefrontCORSPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func originAllowed(allowed map[string]struct{}, origin string) bool {
	_, ok := allowed[origin]
	return ok
}
