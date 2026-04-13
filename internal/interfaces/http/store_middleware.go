package http

import (
	"net/http"

	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// StoreMiddleware resolves the current store from the request host header.
// It looks up the store by domain first; if not found it falls back to the
// default store. If neither is found, the request proceeds without a store
// in the context (downstream handlers may decide how to handle this).
func StoreMiddleware(repo store.StoreRepository, log logger.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := stripPort(r.Host)

			s, err := repo.FindByDomain(r.Context(), host)
			if err != nil {
				log.Error("store.resolve.domain", err, map[string]interface{}{
					"host": host,
				})
			}

			if s == nil {
				s, err = repo.FindDefault(r.Context())
				if err != nil {
					log.Error("store.resolve.default", err, nil)
				}
			}

			if s != nil {
				ctx := store.WithStore(r.Context(), s)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// stripPort removes the port from a host:port string.
func stripPort(host string) string {
	for i := len(host) - 1; i >= 0; i-- {
		if host[i] == ':' {
			return host[:i]
		}
		if host[i] < '0' || host[i] > '9' {
			return host
		}
	}
	return host
}
