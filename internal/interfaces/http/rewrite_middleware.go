package http

import (
	"net/http"

	"github.com/akarso/shopanda/internal/domain/routing"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// ResolverMiddleware looks up the request path in the URL rewrite table.
// If a match is found, the URLRewrite is injected into the request context
// and the request is forwarded to next. If no match is found, a 404 is returned.
func ResolverMiddleware(repo routing.RewriteRepository, log logger.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw, err := repo.FindByPath(r.Context(), r.URL.Path)
			if err != nil {
				log.Error("rewrite_resolve_failed", err, map[string]interface{}{
					"path": r.URL.Path,
				})
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			if rw == nil {
				next.ServeHTTP(w, r)
				return
			}

			ctx := routing.WithRewrite(r.Context(), rw)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
