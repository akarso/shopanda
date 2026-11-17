package pluginsdk

import (
	"context"
	"net/http"

	"github.com/akarso/shopanda/pkg/extapi"
)

// CartExtensionValue is one extension field code/value pair for a cart line.
type CartExtensionValue struct {
	Code  string
	Value interface{}
}

// CartAddItemOpts carries optional extension values for CartPort.AddItem.
// Trusted plugin-only writes require ctx from Host.PluginWriteContext /
// WithPluginWrite naming the field codes being written.
type CartAddItemOpts struct {
	Extensions []CartExtensionValue
	UpdatedBy  string
}

// CartPort is the Host-visible cart surface for plugins that declare CapCart.
//
// EnsureStorefrontCart resolves the same guest cookie as the storefront
// (see pkg/storefrontsession) or the signed-in customer's active cart.
// Do not put CartView.ID into public JSON responses — keep it server-side.
type CartPort interface {
	AddItem(ctx context.Context, cartID, customerID, variantID string, qty int, opts CartAddItemOpts) error
	// Get returns a cart the caller may access (guest: empty customerID).
	// Plugin-only extension values are visible only to the field owner when
	// the port is obtained via AsHostFor / Adapt (scoped Host).
	Get(ctx context.Context, cartID, customerID string) (*extapi.CartView, error)
	// EnsureActive returns the customer's active cart or creates one.
	// customerID must be non-empty.
	EnsureActive(ctx context.Context, customerID, currency string) (*extapi.CartView, error)
	// EnsureStorefrontCart loads or creates the storefront session cart for
	// the request (signed-in → EnsureActive; guest → cookie / create + Set-Cookie).
	// Guest: missing cookie, not-found, or non-active cart → create + Set-Cookie.
	// Other GetCart errors propagate without rewriting the cookie.
	// Blank currency uses store.FromContext then "EUR" (same as storefront).
	EnsureStorefrontCart(ctx context.Context, w http.ResponseWriter, r *http.Request, currency string) (*extapi.CartView, error)
	// DiscardEmptyGuestCart deletes cartID when it is still an empty guest cart
	// (conditional delete) and clears the storefront cookie only when that row
	// was removed. Pass the id returned by EnsureStorefrontCart / CreateCart —
	// not CartIDFromRequest alone (Ensure writes Set-Cookie on the response;
	// the request cookie is unchanged). No-op when the cart is signed-in,
	// missing, has lines, or another request won the race. Prefer calling
	// only when the Ensure snapshot was empty (failed first add).
	DiscardEmptyGuestCart(ctx context.Context, w http.ResponseWriter, r *http.Request, cartID string) error
	// DeleteLineExtension removes one stored value on a line. Plugin-only
	// fields require WithPluginWrite on ctx; scoped Hosts may only delete
	// fields they own.
	DeleteLineExtension(ctx context.Context, cartID, customerID, variantID, fieldCode string) error
	// UpsertLineExtension sets extension values on an existing line without
	// changing quantity. Conflict when a stored string value already differs.
	UpsertLineExtension(ctx context.Context, cartID, customerID, variantID string, values []CartExtensionValue, updatedBy string) error
}

// CatalogPort provides minimal catalog reads for seed and integration plugins.
type CatalogPort interface {
	FindVariant(ctx context.Context, id string) (*extapi.CatalogVariant, error)
	// FindVariants returns variants keyed by id. Missing ids are omitted.
	// Prefer this in cart.validate hooks over per-line FindVariant.
	FindVariants(ctx context.Context, ids []string) (map[string]*extapi.CatalogVariant, error)
	FindProduct(ctx context.Context, id string) (*extapi.CatalogProduct, error)
}

// CatalogProductSeed is the EnsureProduct input for CatalogSeedPort.
// Existing products (matched by Slug) are left unchanged.
type CatalogProductSeed struct {
	Name        string
	Slug        string
	Description string
}

// CatalogVariantSeed is the EnsureVariant input for CatalogSeedPort.
// Existing variants (matched by SKU) must belong to ProductID.
type CatalogVariantSeed struct {
	ProductID string
	SKU       string
	Name      string
}

// CatalogPriceSeed is the UpsertPrice input for CatalogSeedPort.
// Empty StoreID means the global/default price. Amount is minor units.
type CatalogPriceSeed struct {
	VariantID string
	StoreID   string
	Amount    int64
	Currency  string
}

// CatalogSeedPort is the Host-visible catalog write surface for plugin CLI
// seed/import (CapCatalogSeed). It mirrors CatalogSeeder idempotency:
// Ensure* skips when the row already exists; UpsertPrice writes price +
// history; SetStock sets absolute quantity. Distinct from read-only CapCatalog.
type CatalogSeedPort interface {
	EnsureProduct(ctx context.Context, in CatalogProductSeed) (id string, created bool, err error)
	EnsureVariant(ctx context.Context, in CatalogVariantSeed) (id string, created bool, err error)
	UpsertPrice(ctx context.Context, in CatalogPriceSeed) error
	SetStock(ctx context.Context, variantID string, qty int) error
}

// OrderPort provides minimal order reads.
type OrderPort interface {
	FindByID(ctx context.Context, id string) (*extapi.OrderView, error)
}

// CustomerPort provides minimal customer reads.
type CustomerPort interface {
	FindByID(ctx context.Context, id string) (*extapi.CustomerView, error)
}
