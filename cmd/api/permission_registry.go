package main

import (
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// preparePermissionRegistry wires an empty app-owned RBAC registry before InitAll.
func preparePermissionRegistry(app *plugin.App) *rbac.Registry {
	reg := rbac.NewRegistry()
	app.SetPermissionRegistry(reg)
	return reg
}

// freezePermissionRegistry seals plugin permission registration after InitAll.
func freezePermissionRegistry(app *plugin.App) {
	app.FreezePermissionRegistry()
}

// sealPermissionRegistry freezes registration and binds the same instance for
// package-level HasPermission / CatalogPermissions used by HTTP auth (serve only).
func sealPermissionRegistry(app *plugin.App) {
	freezePermissionRegistry(app)
	if reg := app.PermissionRegistry(); reg != nil {
		rbac.BindRuntime(reg)
	}
}
