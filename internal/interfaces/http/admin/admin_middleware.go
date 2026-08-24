package admin

import (
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
)

const (
	adminStoreIDHeader  = "X-Admin-Store-ID"
	adminLanguageHeader = "X-Admin-Language"
	adminCurrencyHeader = "X-Admin-Currency"
	maxAdminScopeLength = 64
)

// RequireRole rejects requests that do not have the specified role.
// Returns 401 for guests and 403 for authenticated users with wrong role.
func RequireRole(role identity.Role) httpshared.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := auth.IdentityFrom(r.Context())
			if id.IsGuest() {
				httpshared.JSONError(w, apperror.Unauthorized("authentication required"))
				return
			}
			if !id.HasRole(role) {
				httpshared.JSONError(w, apperror.Forbidden("insufficient permissions"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequirePermission rejects requests where the caller's role does not
// grant the specified permission.
// Returns 401 for guests and 403 for authenticated users lacking the permission.
func RequirePermission(perm rbac.Permission) httpshared.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := auth.IdentityFrom(r.Context())
			if id.IsGuest() {
				httpshared.JSONError(w, apperror.Unauthorized("authentication required"))
				return
			}
			if !rbac.HasPermission(id.Role, perm) {
				httpshared.JSONError(w, apperror.Forbidden("insufficient permissions"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AdminContextMiddleware injects admin context derived from authenticated
// identity and role permissions. It applies to all authenticated callers
// (admin, manager, editor, support, customer), not just admins. Guest requests
// pass through unchanged. The context contains the caller's ID and resolved
// permissions for that role.
func AdminContextMiddleware() httpshared.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := auth.IdentityFrom(r.Context())
			if id.IsGuest() {
				next.ServeHTTP(w, r)
				return
			}

			perms := rbac.PermissionsForRole(id.Role)
			permStrings := make([]string, 0, len(perms))
			for i := range perms {
				permStrings = append(permStrings, string(perms[i]))
			}

			ctx := (&admin.AdminContext{
				AdminID:     id.UserID,
				Permissions: permStrings,
				StoreID:     sanitizeAdminScopeValue(r.Header.Get(adminStoreIDHeader)),
				Language:    sanitizeAdminScopeValue(r.Header.Get(adminLanguageHeader)),
				Currency:    sanitizeAdminScopeValue(r.Header.Get(adminCurrencyHeader)),
			}).WithContext(r.Context())

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func sanitizeAdminScopeValue(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if len(value) > maxAdminScopeLength {
		return value[:maxAdminScopeLength]
	}
	return value
}
