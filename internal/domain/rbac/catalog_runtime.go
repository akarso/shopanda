package rbac

import (
	"github.com/akarso/shopanda/internal/domain/identity"
)

// Package-level helpers delegate to the process-bound Runtime registry.
//
// Only serve (wireServeRuntime → sealPermissionRegistry) calls BindRuntime.
// CLI/worker/import/export freeze without binding: until BindRuntime, these
// helpers return core-only data (plugin grants empty). Prefer the app-owned
// *Registry from plugin.App.PermissionRegistry() in those processes.

// CatalogPermissions returns core plus runtime plugin permissions.
func CatalogPermissions() []Permission {
	return Runtime().CatalogPermissions()
}

// IsCatalogPermission reports whether perm may be assigned through the roles editor.
func IsCatalogPermission(perm Permission) bool {
	return Runtime().IsCatalogPermission(perm)
}

// DefaultPermissionsForRole returns core defaults plus runtime plugin defaults.
func DefaultPermissionsForRole(role identity.Role) []Permission {
	return Runtime().DefaultPermissionsForRole(role)
}

// PluginPermissions returns runtime plugin permissions (nil registry → empty).
func PluginPermissions() []PluginPermission {
	return Runtime().PluginPermissions()
}
