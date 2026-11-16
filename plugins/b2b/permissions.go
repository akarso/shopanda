package b2b

// Permission strings registered via Host.RegisterPermission. Untyped string
// constants convert implicitly to internal/domain/rbac.Permission at the
// framework boundary (see pluginsdk.Host.RegisterPermission).
const (
	PermissionGroupsRead  = "b2b.groups.read"
	PermissionGroupsWrite = "b2b.groups.write"
	PermissionPricesRead  = "b2b.prices.read"
	PermissionPricesWrite = "b2b.prices.write"
)
