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
	// DiscardEmptyGuestCart deletes the guest session cart when it still has
	// no lines (e.g. after a failed first AddItem) and clears the storefront
	// cookie. Signed-in customers, missing cookies, and carts with items are
	// no-ops. Safe to call on every failed add path.
	DiscardEmptyGuestCart(ctx context.Context, w http.ResponseWriter, r *http.Request) error
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

// OrderPort provides minimal order reads.
type OrderPort interface {
	FindByID(ctx context.Context, id string) (*extapi.OrderView, error)
}

// CustomerPort provides minimal customer reads.
type CustomerPort interface {
	FindByID(ctx context.Context, id string) (*extapi.CustomerView, error)
}
