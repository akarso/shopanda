package rbac

import "github.com/akarso/shopanda/internal/domain/identity"

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
	),
	identity.RoleManager: toSet(
		ProductsRead, ProductsWrite,
		OrdersRead, OrdersWrite,
		CategoriesRead, CategoriesWrite,
		CustomersRead,
		InvoicesRead,
		MediaRead, MediaWrite,
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
func HasPermission(role identity.Role, perm Permission) bool {
	perms, ok := rolePermissions[role]
	if !ok {
		return false
	}
	_, granted := perms[perm]
	return granted
}

// PermissionsForRole returns all permissions granted to a role.
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
	return out
}

func toSet(perms ...Permission) map[Permission]struct{} {
	m := make(map[Permission]struct{}, len(perms))
	for _, p := range perms {
		m[p] = struct{}{}
	}
	return m
}
