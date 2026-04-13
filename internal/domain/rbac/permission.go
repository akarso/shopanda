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

	InvoicesRead Permission = "invoices.read"

	MediaRead  Permission = "media.read"
	MediaWrite Permission = "media.write"

	SettingsRead  Permission = "settings.read"
	SettingsWrite Permission = "settings.write"
)
