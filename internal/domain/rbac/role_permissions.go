package rbac

import (
	"sort"

	"github.com/akarso/shopanda/internal/domain/identity"
)

// rolePermissions maps each admin-level role to its granted permissions.
// The mapping is static; dynamic roles are not supported (see ROLES.md §7).
var rolePermissions = map[identity.Role]map[Permission]struct{}{
	identity.RoleAdmin: toSet(
		ProductsRead, ProductsWrite,
		OrdersRead, OrdersWrite,
		CategoriesRead, CategoriesWrite,
		CustomersRead, CustomersWrite,
		InvoicesRead,
		MediaRead, MediaWrite,
		SettingsRead, SettingsWrite,
		ShippingRead, ShippingWrite,
	),
	identity.RoleManager: toSet(
		ProductsRead, ProductsWrite,
		OrdersRead, OrdersWrite,
		CategoriesRead, CategoriesWrite,
		CustomersRead,
		InvoicesRead,
		MediaRead, MediaWrite,
		ShippingRead, ShippingWrite,
	),
	identity.RoleEditor: toSet(
		ProductsRead, ProductsWrite,
		CategoriesRead, CategoriesWrite,
		MediaRead, MediaWrite,
	),
	identity.RoleSupport: toSet(
		ProductsRead,
		OrdersRead,
		CustomersRead,
		InvoicesRead,
	),
}

// HasPermission reports whether the given role grants the specified permission.
// Checks both core (static) and plugin-registered permissions.
func HasPermission(role identity.Role, perm Permission) bool {
	perms, ok := rolePermissions[role]
	if ok {
		if _, granted := perms[perm]; granted {
			return true
		}
	}
	return hasPluginPermission(role, perm)
}

// PermissionsForRole returns all permissions granted to a role.
// The result is sorted lexicographically for deterministic output.
// Returns nil for unrecognised roles.
func PermissionsForRole(role identity.Role) []Permission {
	perms, ok := rolePermissions[role]
	if !ok {
		return nil
	}
	out := make([]Permission, 0, len(perms))
	for p := range perms {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func toSet(perms ...Permission) map[Permission]struct{} {
	m := make(map[Permission]struct{}, len(perms))
	for _, p := range perms {
		m[p] = struct{}{}
	}
	return m
}
