package storefront

import (
	"context"
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
// Order for Authorization:
//  1. Valid Bearer customer JWT → that identity (session cookie ignored).
//  2. Bearer that is not a customer JWT (auth.ErrNotCustomerToken / opaque
//     owner token) → storefront session cookie if valid, else guest (so
//     plugin routes can still open the guest cart).
//  3. Bearer that fails as a customer JWT (expired, revoked, tampered) → 401;
//     the session cookie is not consulted.
//  4. Non-Bearer Authorization → session cookie if valid, else guest.
//  5. No Authorization → session cookie if valid, else guest.
func AuthMiddleware(parser auth.TokenParser) httpshared.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if id, ok, hardFail := tryBearerIdentity(r, parser); ok {
				next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), id)))
				return
			} else if hardFail {
				httpshared.JSONError(w, apperror.Unauthorized("invalid or expired token"))
				return
			}

			if id, ok := trySessionIdentity(r, parser); ok {
				next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), id)))
				return
			}

			next.ServeHTTP(w, r.WithContext(ensureGuestContext(r.Context())))
		})
	}
}

// tryBearerIdentity parses Authorization when present.
// ok=true → use id. hardFail=true → respond 401 (failed customer JWT).
// Both false → continue (no bearer, or opaque non-customer bearer).
func tryBearerIdentity(r *http.Request, parser auth.TokenParser) (identity.Identity, bool, bool) {
	raw := strings.TrimSpace(r.Header.Get("Authorization"))
	if raw == "" {
		return identity.Identity{}, false, false
	}
	scheme, token, ok := splitAuth(raw)
	if !ok {
		return identity.Identity{}, false, false
	}
	if !strings.EqualFold(scheme, "Bearer") {
		return identity.Identity{}, false, false
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return identity.Identity{}, false, true
	}
	id, err := parser.Parse(r.Context(), token)
	if err == nil {
		return id, true, false
	}
	if auth.IsNotCustomerToken(err) {
		return identity.Identity{}, false, false
	}
	return identity.Identity{}, false, true
}

func trySessionIdentity(r *http.Request, parser auth.TokenParser) (identity.Identity, bool) {
	token := storefrontsession.SessionTokenFromRequest(r)
	if token == "" {
		return identity.Identity{}, false
	}
	id, err := parser.Parse(r.Context(), token)
	if err != nil {
		return identity.Identity{}, false
	}
	return id, true
}

func splitAuth(header string) (scheme, token string, ok bool) {
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	scheme = strings.TrimSpace(parts[0])
	token = parts[1]
	if scheme == "" {
		return "", "", false
	}
	return scheme, token, true
}

func ensureGuestContext(ctx context.Context) context.Context {
	if existing := auth.IdentityFrom(ctx); !existing.IsGuest() {
		return ctx
	}
	return auth.WithIdentity(ctx, identity.Guest())
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
