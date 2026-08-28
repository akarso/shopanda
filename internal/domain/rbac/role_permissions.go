package rbac

import (
	"sort"

	"github.com/akarso/shopanda/internal/domain/identity"
)

// rolePermissions maps each admin-level role to its compiled-in core permissions.
// DB-backed overrides loaded via InitEffectivePermissions take precedence at runtime.
var rolePermissions = map[identity.Role]map[Permission]struct{}{
	identity.RoleAdmin: toSet(
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
		JobsRead, JobsWrite,
	),
	identity.RoleManager: toSet(
		ProductsRead, ProductsWrite,
		OrdersRead, OrdersWrite,
		CategoriesRead, CategoriesWrite,
		CustomersRead,
		InvoicesRead,
		MediaRead, MediaWrite,
		ContentRead,
		ShippingRead, ShippingWrite,
	),
	identity.RoleEditor: toSet(
		ProductsRead, ProductsWrite,
		CategoriesRead, CategoriesWrite,
		MediaRead, MediaWrite,
		ContentRead, ContentWrite,
	),
	identity.RoleSupport: toSet(
		ProductsRead,
		OrdersRead,
		CustomersRead,
		InvoicesRead,
		ContentRead,
	),
}

// HasPermission reports whether the given role grants the specified permission.
func HasPermission(role identity.Role, perm Permission) bool {
	if set, initialized := effectiveAccess(role); initialized {
		_, granted := set[perm]
		return granted
	}
	return staticHasPermission(role, perm)
}

// PermissionsForRole returns all permissions granted to a role.
// The result is sorted lexicographically for deterministic output.
// Returns nil for unrecognised roles.
func PermissionsForRole(role identity.Role) []Permission {
	if set, initialized := effectiveAccess(role); initialized {
		out := make([]Permission, 0, len(set))
		for p := range set {
			out = append(out, p)
		}
		sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
		return out
	}
	return staticPermissionsForRole(role)
}

func toSet(perms ...Permission) map[Permission]struct{} {
	m := make(map[Permission]struct{}, len(perms))
	for _, p := range perms {
		m[p] = struct{}{}
	}
	return m
}
