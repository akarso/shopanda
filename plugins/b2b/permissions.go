package b2b

import "github.com/akarso/shopanda/internal/domain/rbac"

const (
	PermissionGroupsRead  rbac.Permission = "b2b.groups.read"
	PermissionGroupsWrite rbac.Permission = "b2b.groups.write"
	PermissionPricesRead  rbac.Permission = "b2b.prices.read"
	PermissionPricesWrite rbac.Permission = "b2b.prices.write"
)
