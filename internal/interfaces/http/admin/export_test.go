package admin

import (
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
)

// RequirePermissionUsing is the test hook for RequirePermission with a
// caller-supplied checker so tests do not mutate rbac.effectivePerms.
func RequirePermissionUsing(has func(identity.Role, rbac.Permission) bool, perm rbac.Permission) httpshared.Middleware {
	return requirePermission(has, perm)
}

// RequireAnyPermissionUsing is the test hook for RequireAnyPermission.
func RequireAnyPermissionUsing(has func(identity.Role, rbac.Permission) bool, perms ...rbac.Permission) httpshared.Middleware {
	return requireAnyPermission(has, perms...)
}

// SetCacheAdminPermissionCheck installs a test-local checker on this
// handler instance. Production wiring keeps rbac.HasPermission.
func SetCacheAdminPermissionCheck(h *CacheAdminHandler, has func(identity.Role, rbac.Permission) bool) {
	if has == nil {
		has = rbac.HasPermission
	}
	h.hasPerm = has
}
