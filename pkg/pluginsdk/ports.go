package pluginsdk

import (
	"context"
	"net/http"
	"time"

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

// CatalogProductSeed is the EnsureProduct / Import input for CatalogSeedPort.
// Create-only: an existing slug is not updated. When Status or Type is set and
// disagrees with the stored row, or Attributes fails the match rules below,
// Ensure / Import returns an error instead of soft-succeeding (created=false).
// Name / Description on an existing slug are still soft-ignored.
//
// Status is case-sensitive (draft|active|archived). Empty / whitespace → draft
// on create (breaking vs PR-1564, which forced active). Published rows need
// Status: "active" explicitly. Type is case-sensitive; empty / whitespace →
// simple. Attributes is deep-copied via JSON (nil leaves NewProduct's empty
// map; non-nil including {} replaces it on create).
//
// Attributes match on existing slug: nil = do not check; {} = assert stored
// attributes are empty; non-empty = every seed key must equal the stored
// value (extra stored keys from admin/CSV are OK).
//
// Downloadable + active is rejected (no file attachments on seed; same as
// CSV/admin). Active bundle/grouped/configurable rows are allowed; seed does
// not compose children — the operator owns that integrity.
type CatalogProductSeed struct {
	Name        string
	Slug        string
	Description string
	Status      string         // draft|active|archived; empty → draft; case-sensitive
	Type        string         // simple|virtual|…; empty → simple; case-sensitive
	Attributes  map[string]any // optional; deep-copied; see match rules above
}

// CatalogVariantSeed is the EnsureVariant / Import input for CatalogSeedPort.
// Create-only by SKU. ProductID or ProductSlug (resolved against Import's
// product map) identifies the parent. Attributes match rules match
// CatalogProductSeed (nil / {} / subset keys). Deep-copied onto create.
type CatalogVariantSeed struct {
	ProductID   string
	ProductSlug string
	SKU         string
	Name        string
	Attributes  map[string]any
}

// CatalogPriceSeed is the EnsurePrice / Import input. Empty StoreID means the
// global/default price. Amount is minor units (use extapi.ParseMinorUnits).
// Default (Update=false) is create-only: an existing variant+currency+store
// row is left unchanged (soft no-op before money validation). When Update is
// true, a differing amount is written and a price-history snapshot is
// recorded in the same transaction when db+WithTx are bound (same amount is
// a no-op). Missing rows are always created regardless of Update.
//
// Seed price writes do not emit pricing.EventPriceUpserted and do not run
// admin LockTypeAndModes guards (grouped / dynamic-bundle rejection). Prefer
// Import for large loops; after Update re-seeds, operators must reindex /
// invalidate (see EXTENSION_POINTS / RUNBOOK).
type CatalogPriceSeed struct {
	VariantID  string
	VariantSKU string // optional Import helper when VariantID is empty
	StoreID    string
	Amount     int64
	Currency   string
	Update     bool // when true, change amount + history; default create-only
}

// CatalogStockSeed is the Import stock row. Prefer VariantID; VariantSKU is
// resolved from Import's variant map when VariantID is empty.
type CatalogStockSeed struct {
	VariantID  string
	VariantSKU string
	Qty        int
}

// CatalogSeedBatch is the single-call Import payload for plugin CLI seed.
type CatalogSeedBatch struct {
	Products []CatalogProductSeed
	Variants []CatalogVariantSeed
	Prices   []CatalogPriceSeed
	Stock    []CatalogStockSeed
}

// CatalogSeedResult maps slugs/SKUs to ids created or found by Import.
type CatalogSeedResult struct {
	ProductIDs map[string]string // slug → id
	VariantIDs map[string]string // sku → id
}

// CatalogSeedPort is the Host-visible catalog write surface for plugin CLI
// seed/import (CapCatalogSeed). Products (slug) and variants (SKU) are
// create-only. Prices are create-only unless CatalogPriceSeed.Update is set.
// When a seed supplies Status / Type / Attributes (product) or Attributes
// (variant) that disagree with the stored row, the call errors — create-only
// must not pretend Ensure applied those fields. Import runs the batch in one
// DB transaction when a tx starter is bound. Distinct from read-only
// CapCatalog.
type CatalogSeedPort interface {
	// Import applies Products → Variants → Prices → Stock in one transaction
	// when possible. Prefer this from plugin CLI seed commands.
	Import(ctx context.Context, batch CatalogSeedBatch) (*CatalogSeedResult, error)

	EnsureProduct(ctx context.Context, in CatalogProductSeed) (id string, created bool, err error)
	EnsureVariant(ctx context.Context, in CatalogVariantSeed) (id string, created bool, err error)
	// EnsurePrice returns written=true only after a successful create or
	// amount-changing Update (history recorded). written=false on create-only
	// hit, same-amount Update, or any error. Breaking vs PR-1564's created
	// flag: true no longer means "row inserted" only — Update re-seeds that
	// change the amount also return true.
	EnsurePrice(ctx context.Context, in CatalogPriceSeed) (written bool, err error)
	SetStock(ctx context.Context, variantID string, qty int) error
}

// OrderPort provides order reads for plugins that declare CapOrders.
//
// Lines returns each order line keyed by (orderID, VariantID) — items have no
// separate id. Extension values are public fields plus PluginOnlyWrite fields
// owned by the calling plugin (Host from AsHostFor / Adapt); without an
// extension reader attached at the composition root, Extensions is empty.
// ListCreatedSince is for reconciliation (CreatedAt >= since, keyset via
// afterID); CreatedAt is included on each OrderView for cursor display.
type OrderPort interface {
	FindByID(ctx context.Context, id string) (*extapi.OrderView, error)
	Lines(ctx context.Context, orderID string) ([]extapi.OrderLineView, error)
	ListCreatedSince(ctx context.Context, since time.Time, afterID string, limit int) ([]extapi.OrderView, error)
}

// CustomerPort provides minimal customer reads.
type CustomerPort interface {
	FindByID(ctx context.Context, id string) (*extapi.CustomerView, error)
}
