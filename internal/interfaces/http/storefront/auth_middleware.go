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
//  1. Valid Bearer with RoleCustomer → that identity (session cookie ignored).
//  2. Valid Bearer with a staff role → try session cookie first (so an admin
//     API token does not replace a shopper session); if no session, use the
//     staff identity (staff may shop on their own account).
//  3. Bearer that is not a customer JWT (auth.ErrNotCustomerToken / opaque
//     owner token — wrong shape, not three JWT segments) → session cookie if
//     valid, else guest. Two-dot bearers that fail signature/expiry stay 401.
//  4. Bearer that fails as a customer JWT (expired, revoked, tampered) → 401;
//     the session cookie is not consulted.
//  5. Non-Bearer Authorization → session cookie if valid, else guest.
//  6. No Authorization → session cookie if valid, else guest.
func AuthMiddleware(parser auth.TokenParser) httpshared.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bearer, hardFail, staffFallback, hasStaff := tryBearerIdentity(r, parser)
			if hardFail {
				httpshared.JSONError(w, apperror.Unauthorized("invalid or expired token"))
				return
			}
			if bearer != nil {
				next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), *bearer)))
				return
			}

			if id, ok := trySessionIdentity(r, parser); ok {
				next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), id)))
				return
			}

			if hasStaff {
				next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), staffFallback)))
				return
			}

			next.ServeHTTP(w, r.WithContext(ensureGuestContext(r.Context())))
		})
	}
}

// tryBearerIdentity parses Authorization when present.
// bearer non-nil → use immediately (RoleCustomer).
// hardFail → 401. hasStaff → parsed staff identity deferred behind session.
func tryBearerIdentity(r *http.Request, parser auth.TokenParser) (bearer *identity.Identity, hardFail bool, staff identity.Identity, hasStaff bool) {
	raw := strings.TrimSpace(r.Header.Get("Authorization"))
	if raw == "" {
		return nil, false, identity.Identity{}, false
	}
	scheme, token, ok := splitAuth(raw)
	if !ok {
		return nil, false, identity.Identity{}, false
	}
	if !strings.EqualFold(scheme, "Bearer") {
		return nil, false, identity.Identity{}, false
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, true, identity.Identity{}, false
	}
	id, err := parser.Parse(r.Context(), token)
	if err != nil {
		if auth.IsNotCustomerToken(err) {
			return nil, false, identity.Identity{}, false
		}
		return nil, true, identity.Identity{}, false
	}
	if id.HasRole(identity.RoleCustomer) {
		return &id, false, identity.Identity{}, false
	}
	// Staff (or other non-customer) bearer: prefer session cookie when present.
	return nil, false, id, true
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
	parts := strings.SplitN(strings.TrimSpace(header), " ", 2)
	scheme = strings.TrimSpace(parts[0])
	if scheme == "" {
		return "", "", false
	}
	if len(parts) == 1 {
		return scheme, "", true
	}
	return scheme, parts[1], true
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
