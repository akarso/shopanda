package rbac

import (
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
		CustomersRead, CustomersWrite, StoreCreditWrite,
		InvoicesRead,
		MediaRead, MediaWrite,
		ContentRead, ContentWrite,
		SettingsRead, SettingsWrite,
		ShippingRead, ShippingWrite,
		AuditRead,
		ExtensionsRead, ExtensionsWrite, ExtensionsPrivateRead,
	}
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
