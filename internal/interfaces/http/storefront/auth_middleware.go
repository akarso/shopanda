package storefront

import (
	"net/http"
	"strings"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
)

// AuthMiddleware parses the Authorization header and injects an Identity
// into the request context. If no token is present, a guest identity is
// injected. If the token is invalid, a 401 response is returned.
func AuthMiddleware(parser auth.TokenParser) httpshared.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				if token := storefrontSessionToken(r); token != "" {
					id, err := parser.Parse(r.Context(), token)
					if err == nil {
						ctx := auth.WithIdentity(r.Context(), id)
						next.ServeHTTP(w, r.WithContext(ctx))
						return
					}
				}
				if existing := auth.IdentityFrom(r.Context()); !existing.IsGuest() {
					next.ServeHTTP(w, r)
					return
				}
				ctx := auth.WithIdentity(r.Context(), identity.Guest())
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			if !strings.HasPrefix(header, "Bearer ") {
				httpshared.JSONError(w, apperror.Unauthorized("invalid authorization header"))
				return
			}
			token := header[len("Bearer "):]

			id, err := parser.Parse(r.Context(), token)
			if err != nil {
				httpshared.JSONError(w, apperror.Unauthorized("invalid or expired token"))
				return
			}

			ctx := auth.WithIdentity(r.Context(), id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAuth rejects unauthenticated (guest) requests with a 401 response.
func RequireAuth() httpshared.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := auth.IdentityFrom(r.Context())
			if id.IsGuest() {
				httpshared.JSONError(w, apperror.Unauthorized("authentication required"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
