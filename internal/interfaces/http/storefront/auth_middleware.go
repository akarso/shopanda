package storefront

import (
	"net/http"
	"strings"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
	"github.com/akarso/shopanda/pkg/storefrontsession"
)

// AuthMiddleware parses optional customer identity into the request context.
//
// Order: valid Authorization Bearer customer JWT → storefront session cookie →
// guest. A Bearer that fails to parse as a customer JWT (e.g. a plugin owner
// token) does not 401 when a valid session cookie is present; only a failed
// customer Bearer with no usable session returns 401. Non-Bearer Authorization
// schemes are left for the handler and do not block session-cookie identity.
func AuthMiddleware(parser auth.TokenParser) httpshared.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bearerFailed := false
			header := r.Header.Get("Authorization")
			if strings.HasPrefix(header, "Bearer ") {
				token := header[len("Bearer "):]
				id, err := parser.Parse(r.Context(), token)
				if err == nil {
					ctx := auth.WithIdentity(r.Context(), id)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				bearerFailed = true
			} else if header != "" && !strings.HasPrefix(header, "Bearer ") {
				// Non-Bearer scheme (owner token, Basic, …): do not 401; try session.
			}

			if token := storefrontsession.SessionTokenFromRequest(r); token != "" {
				id, err := parser.Parse(r.Context(), token)
				if err == nil {
					ctx := auth.WithIdentity(r.Context(), id)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			if bearerFailed {
				httpshared.JSONError(w, apperror.Unauthorized("invalid or expired token"))
				return
			}
			if header != "" && !strings.HasPrefix(header, "Bearer ") {
				// Non-Bearer Authorization with no session: continue as guest so
				// plugin routes can authenticate via their own scheme.
				if existing := auth.IdentityFrom(r.Context()); !existing.IsGuest() {
					next.ServeHTTP(w, r)
					return
				}
				ctx := auth.WithIdentity(r.Context(), identity.Guest())
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			if existing := auth.IdentityFrom(r.Context()); !existing.IsGuest() {
				next.ServeHTTP(w, r)
				return
			}
			ctx := auth.WithIdentity(r.Context(), identity.Guest())
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
