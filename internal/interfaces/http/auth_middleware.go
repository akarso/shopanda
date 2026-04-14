package http

import (
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
)

// AuthMiddleware parses the Authorization header and injects an Identity
// into the request context. If no token is present, a guest identity is
// injected. If the token is invalid, a 401 response is returned.
func AuthMiddleware(parser auth.TokenParser) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				ctx := auth.WithIdentity(r.Context(), identity.Guest())
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			if !strings.HasPrefix(header, "Bearer ") {
				JSONError(w, apperror.Unauthorized("invalid authorization header"))
				return
			}
			token := header[len("Bearer "):]

			id, err := parser.Parse(r.Context(), token)
			if err != nil {
				JSONError(w, apperror.Unauthorized("invalid or expired token"))
				return
			}

			ctx := auth.WithIdentity(r.Context(), id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAuth rejects unauthenticated (guest) requests with a 401 response.
func RequireAuth() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := auth.IdentityFrom(r.Context())
			if id.IsGuest() {
				JSONError(w, apperror.Unauthorized("authentication required"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireRole rejects requests that do not have the specified role.
// Returns 401 for guests and 403 for authenticated users with wrong role.
func RequireRole(role identity.Role) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := auth.IdentityFrom(r.Context())
			if id.IsGuest() {
				JSONError(w, apperror.Unauthorized("authentication required"))
				return
			}
			if !id.HasRole(role) {
				JSONError(w, apperror.Forbidden("insufficient permissions"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequirePermission rejects requests where the caller's role does not
// grant the specified permission.
// Returns 401 for guests and 403 for authenticated users lacking the permission.
func RequirePermission(perm rbac.Permission) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := auth.IdentityFrom(r.Context())
			if id.IsGuest() {
				JSONError(w, apperror.Unauthorized("authentication required"))
				return
			}
			if !rbac.HasPermission(id.Role, perm) {
				JSONError(w, apperror.Forbidden("insufficient permissions"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
