package rbac

// Permission represents a named action that can be guarded.
type Permission string

// Permissions used by the core system. Plugins may define additional
// permissions and register them at startup (see PR-103).
const (
	ProductsRead  Permission = "products.read"
	ProductsWrite Permission = "products.write"

	OrdersRead  Permission = "orders.read"
	OrdersWrite Permission = "orders.write"

	CategoriesRead  Permission = "categories.read"
	CategoriesWrite Permission = "categories.write"

	CustomersRead  Permission = "customers.read"
	CustomersWrite Permission = "customers.write"

	// StoreCreditWrite gates issuing store credit — kept distinct from
	// CustomersWrite so a role granted customer-profile editing for CRM
	// purposes does not implicitly gain the ability to mint store credit.
	StoreCreditWrite Permission = "customers.store_credit.write"

	InvoicesRead Permission = "invoices.read"

	MediaRead  Permission = "media.read"
	MediaWrite Permission = "media.write"

	ContentRead  Permission = "content.read"
	ContentWrite Permission = "content.write"

	SettingsRead  Permission = "settings.read"
	SettingsWrite Permission = "settings.write"

	ShippingRead  Permission = "shipping.read"
	ShippingWrite Permission = "shipping.write"

	AuditRead Permission = "audit.read"

	ExtensionsRead        Permission = "extensions.read"
	ExtensionsWrite       Permission = "extensions.write"
	ExtensionsPrivateRead Permission = "extensions.private.read"

	// JobsRead/JobsWrite gate background-job introspection and admin
	// corrections (list/detail vs. retry/cancel) — operational visibility
	// into the job queue, not a merchant-facing resource, so these are
	// admin-only (see rolePermissions) the same way AuditRead is.
	JobsRead  Permission = "jobs.read"
	JobsWrite Permission = "jobs.write"
)
