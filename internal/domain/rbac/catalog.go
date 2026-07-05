package rbac

import (
	"sort"

	"github.com/akarso/shopanda/internal/domain/identity"
)

// PluginPermission describes a plugin-registered permission and its default roles.
type PluginPermission struct {
	Permission   Permission
	DefaultRoles []identity.Role
}

// AdminRoles returns the fixed admin-panel roles that may be customized.
func AdminRoles() []identity.Role {
	return []identity.Role{
		identity.RoleAdmin,
		identity.RoleManager,
		identity.RoleEditor,
		identity.RoleSupport,
	}
}

// CorePermissions returns all core permission constants.
func CorePermissions() []Permission {
	return []Permission{
		ProductsRead, ProductsWrite,
		OrdersRead, OrdersWrite,
		CategoriesRead, CategoriesWrite,
		CustomersRead, CustomersWrite,
		InvoicesRead,
		MediaRead, MediaWrite,
		ContentRead, ContentWrite,
		SettingsRead, SettingsWrite,
		ShippingRead, ShippingWrite,
		AuditRead,
		ExtensionsRead, ExtensionsWrite, ExtensionsPrivateRead,
	}
}

// CatalogPermissions returns core plus registered plugin permissions sorted lexicographically.
func CatalogPermissions() []Permission {
	core := CorePermissions()
	seen := make(map[Permission]struct{}, len(core)+8)
	out := make([]Permission, 0, len(core)+8)
	for _, p := range core {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	for p := range pluginPermissionSnapshot() {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// IsAdminRole reports whether role is one of the customizable admin roles.
func IsAdminRole(role identity.Role) bool {
	switch role {
	case identity.RoleAdmin, identity.RoleManager, identity.RoleEditor, identity.RoleSupport:
		return true
	default:
		return false
	}
}

// IsCatalogPermission reports whether perm may be assigned through the roles editor.
func IsCatalogPermission(perm Permission) bool {
	for _, p := range CatalogPermissions() {
		if p == perm {
			return true
		}
	}
	return false
}

// DefaultPermissionsForRole returns the compiled-in default permissions for a role.
func DefaultPermissionsForRole(role identity.Role) []Permission {
	perms, ok := rolePermissions[role]
	if !ok {
		return nil
	}
	out := make([]Permission, 0, len(perms))
	for p := range perms {
		out = append(out, p)
	}
	for _, entry := range PluginPermissions() {
		for _, r := range entry.DefaultRoles {
			if r == role {
				out = append(out, entry.Permission)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
