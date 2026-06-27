package plugin

import (
	"net/http"

	"github.com/akarso/shopanda/internal/domain/rbac"
)

// AdminRoute registers a permission-guarded admin HTTP handler.
type AdminRoute struct {
	Pattern    string
	Permission rbac.Permission
	Handler    http.Handler
}

// RegisterAdminRoute adds a route mounted by the application after InitAll.
func (a *App) RegisterAdminRoute(pattern string, perm rbac.Permission, handler http.Handler) {
	if pattern == "" {
		panic("plugin: admin route pattern must not be empty")
	}
	if handler == nil {
		panic("plugin: admin route handler must not be nil")
	}
	if perm == "" {
		panic("plugin: admin route permission must not be empty")
	}
	a.adminRoutes = append(a.adminRoutes, AdminRoute{
		Pattern:    pattern,
		Permission: perm,
		Handler:    handler,
	})
}

// AdminRoutes returns registered admin routes.
func (a *App) AdminRoutes() []AdminRoute {
	if len(a.adminRoutes) == 0 {
		return nil
	}
	out := make([]AdminRoute, len(a.adminRoutes))
	copy(out, a.adminRoutes)
	return out
}
