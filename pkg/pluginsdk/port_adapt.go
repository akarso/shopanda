package pluginsdk

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	domaincart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domaincustomer "github.com/akarso/shopanda/internal/domain/customer"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/domain/inventory"
	domainorder "github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/storefrontsession"
)

// NewCatalogPort adapts catalog repositories to CatalogPort.
func NewCatalogPort(products catalog.ProductRepository, variants catalog.VariantRepository) CatalogPort {
	return &catalogPort{products: products, variants: variants}
}

type catalogPort struct {
	products catalog.ProductRepository
	variants catalog.VariantRepository
}

func (p *catalogPort) FindVariant(ctx context.Context, id string) (*extapi.CatalogVariant, error) {
	if p == nil || p.variants == nil {
		return nil, fmt.Errorf("pluginsdk: catalog port: variants not configured")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		// Same as FindVariants: empty id is a miss, not a repository call.
		return nil, nil
	}
	out, err := p.FindVariants(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	return out[id], nil
}

func (p *catalogPort) FindVariants(ctx context.Context, ids []string) (map[string]*extapi.CatalogVariant, error) {
	if p == nil || p.variants == nil {
		return nil, fmt.Errorf("pluginsdk: catalog port: variants not configured")
	}
	variants, err := catalog.LookupVariants(ctx, p.variants, ids)
	if err != nil {
		return nil, err
	}
	productIDs := make([]string, 0, len(variants))
	seenProduct := make(map[string]struct{}, len(variants))
	for _, v := range variants {
		if v == nil || strings.TrimSpace(v.ProductID) == "" {
			continue
		}
		if _, ok := seenProduct[v.ProductID]; ok {
			continue
		}
		seenProduct[v.ProductID] = struct{}{}
		productIDs = append(productIDs, v.ProductID)
	}
	products, err := catalog.LookupProducts(ctx, p.products, productIDs)
	if err != nil {
		return nil, fmt.Errorf("pluginsdk: catalog port: parent products: %w", err)
	}

	out := make(map[string]*extapi.CatalogVariant, len(variants))
	for reqID, v := range variants {
		if v == nil {
			continue
		}
		cv := &extapi.CatalogVariant{
			ID:        v.ID,
			ProductID: v.ProductID,
			SKU:       v.SKU,
			Name:      v.Name,
		}
		if prod := products[v.ProductID]; prod != nil {
			cv.ProductVisible = true
			cv.ProductSlug = prod.Slug
		}
		// !ProductVisible: parent missing, non-active / visibility-gated under
		// storefront read scope, or products repo nil. Hooks must not treat
		// empty ProductSlug alone as "not my product" (fail-open).
		out[reqID] = cv
	}
	return out, nil
}

func (p *catalogPort) FindProduct(ctx context.Context, id string) (*extapi.CatalogProduct, error) {
	if p == nil || p.products == nil {
		return nil, fmt.Errorf("pluginsdk: catalog port: products not configured")
	}
	prod, err := p.products.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if prod == nil {
		return nil, nil
	}
	return &extapi.CatalogProduct{
		ID:     prod.ID,
		Slug:   prod.Slug,
		Name:   prod.Name,
		Type:   string(prod.Type),
		Status: string(prod.Status),
	}, nil
}

// catalogSeedTxStarter begins a transaction for atomic price + history writes.
type catalogSeedTxStarter interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

type catalogSeedTxProducts interface {
	catalog.ProductRepository
	WithTx(tx *sql.Tx) catalog.ProductRepository
}

type catalogSeedTxVariants interface {
	catalog.VariantRepository
	WithTx(tx *sql.Tx) catalog.VariantRepository
}

type catalogSeedTxPrice interface {
	pricing.PriceRepository
	WithTx(tx *sql.Tx) pricing.PriceRepository
}

type catalogSeedTxHistory interface {
	pricing.PriceHistoryRepository
	WithTx(tx *sql.Tx) pricing.PriceHistoryRepository
}

type catalogSeedTxStock interface {
	inventory.StockRepository
	WithTx(tx *sql.Tx) inventory.StockRepository
}

// NewCatalogSeedPort adapts catalog/pricing/inventory repositories to CatalogSeedPort.
// When db is set and repos support WithTx (postgres), Import uses one transaction.
func NewCatalogSeedPort(
	products catalog.ProductRepository,
	variants catalog.VariantRepository,
	prices pricing.PriceRepository,
	history pricing.PriceHistoryRepository,
	stock inventory.StockRepository,
	db catalogSeedTxStarter,
) CatalogSeedPort {
	return &catalogSeedPort{
		products: products,
		variants: variants,
		prices:   prices,
		history:  history,
		stock:    stock,
		db:       db,
	}
}

type catalogSeedPort struct {
	products catalog.ProductRepository
	variants catalog.VariantRepository
	prices   pricing.PriceRepository
	history  pricing.PriceHistoryRepository
	stock    inventory.StockRepository
	db       catalogSeedTxStarter
}

func (p *catalogSeedPort) withTx(tx *sql.Tx) (*catalogSeedPort, error) {
	out := *p
	if tp, ok := p.products.(catalogSeedTxProducts); ok {
		out.products = tp.WithTx(tx)
	} else if p.products != nil {
		return nil, fmt.Errorf("pluginsdk: catalog seed port: products repo does not support WithTx")
	}
	if tv, ok := p.variants.(catalogSeedTxVariants); ok {
		out.variants = tv.WithTx(tx)
	} else if p.variants != nil {
		return nil, fmt.Errorf("pluginsdk: catalog seed port: variants repo does not support WithTx")
	}
	if tp, ok := p.prices.(catalogSeedTxPrice); ok {
		out.prices = tp.WithTx(tx)
	} else if p.prices != nil {
		return nil, fmt.Errorf("pluginsdk: catalog seed port: prices repo does not support WithTx")
	}
	if th, ok := p.history.(catalogSeedTxHistory); ok {
		out.history = th.WithTx(tx)
	} else if p.history != nil {
		return nil, fmt.Errorf("pluginsdk: catalog seed port: price history repo does not support WithTx")
	}
	if ts, ok := p.stock.(catalogSeedTxStock); ok {
		out.stock = ts.WithTx(tx)
	} else if p.stock != nil {
		return nil, fmt.Errorf("pluginsdk: catalog seed port: stock repo does not support WithTx")
	}
	out.db = nil // nested Import must not open another tx
	return &out, nil
}

// Import applies the seed batch in one transaction when db + WithTx repos are bound.
func (p *catalogSeedPort) Import(ctx context.Context, batch CatalogSeedBatch) (*CatalogSeedResult, error) {
	if p == nil {
		return nil, fmt.Errorf("pluginsdk: catalog seed port: not configured")
	}
	run := p
	var tx *sql.Tx
	if p.db != nil {
		var err error
		tx, err = p.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("pluginsdk: catalog seed port: begin tx: %w", err)
		}
		defer func() {
			if tx != nil {
				_ = tx.Rollback()
			}
		}()
		run, err = p.withTx(tx)
		if err != nil {
			return nil, err
		}
	}

	result := &CatalogSeedResult{
		ProductIDs: make(map[string]string, len(batch.Products)),
		VariantIDs: make(map[string]string, len(batch.Variants)),
	}
	for _, prod := range batch.Products {
		id, _, err := run.EnsureProduct(ctx, prod)
		if err != nil {
			return nil, err
		}
		result.ProductIDs[strings.TrimSpace(prod.Slug)] = id
	}
	for _, v := range batch.Variants {
		in := v
		if strings.TrimSpace(in.ProductID) == "" {
			slug := strings.TrimSpace(in.ProductSlug)
			pid, ok := result.ProductIDs[slug]
			if !ok || pid == "" {
				return nil, fmt.Errorf("pluginsdk: catalog seed port: variant SKU %q: unknown product slug %q", in.SKU, slug)
			}
			in.ProductID = pid
		}
		id, _, err := run.EnsureVariant(ctx, in)
		if err != nil {
			return nil, err
		}
		result.VariantIDs[strings.TrimSpace(in.SKU)] = id
	}
	for _, price := range batch.Prices {
		in := price
		if strings.TrimSpace(in.VariantID) == "" {
			sku := strings.TrimSpace(in.VariantSKU)
			vid, ok := result.VariantIDs[sku]
			if !ok || vid == "" {
				return nil, fmt.Errorf("pluginsdk: catalog seed port: price: unknown variant sku %q", sku)
			}
			in.VariantID = vid
		}
		if _, err := run.EnsurePrice(ctx, in); err != nil {
			return nil, err
		}
	}
	for _, st := range batch.Stock {
		vid := strings.TrimSpace(st.VariantID)
		if vid == "" {
			sku := strings.TrimSpace(st.VariantSKU)
			id, ok := result.VariantIDs[sku]
			if !ok || id == "" {
				return nil, fmt.Errorf("pluginsdk: catalog seed port: stock: unknown variant sku %q", sku)
			}
			vid = id
		}
		if err := run.SetStock(ctx, vid, st.Qty); err != nil {
			return nil, err
		}
	}

	if tx != nil {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("pluginsdk: catalog seed port: commit: %w", err)
		}
		tx = nil
	}
	return result, nil
}

func (p *catalogSeedPort) EnsureProduct(ctx context.Context, in CatalogProductSeed) (string, bool, error) {
	if p == nil || p.products == nil {
		return "", false, fmt.Errorf("pluginsdk: catalog seed port: products not configured")
	}
	slug := strings.TrimSpace(in.Slug)
	if slug == "" {
		return "", false, fmt.Errorf("pluginsdk: catalog seed port: product slug must not be empty")
	}
	ctx = catalog.WithOperatorProductReadScope(ctx)
	existing, err := p.products.FindBySlug(ctx, slug)
	if err != nil {
		return "", false, err
	}
	if existing != nil {
		return existing.ID, false, nil
	}

	prod, err := catalog.NewProduct(id.New(), strings.TrimSpace(in.Name), slug)
	if err != nil {
		return "", false, err
	}
	prod.Description = strings.TrimSpace(in.Description)
	prod.Status = catalog.StatusActive

	if err := p.products.Create(ctx, &prod); err != nil {
		if apperror.Is(err, apperror.CodeConflict) {
			ret, err2 := p.products.FindBySlug(ctx, slug)
			if err2 != nil {
				return "", false, err2
			}
			if ret == nil {
				return "", false, err
			}
			return ret.ID, false, nil
		}
		return "", false, err
	}
	return prod.ID, true, nil
}

func (p *catalogSeedPort) EnsureVariant(ctx context.Context, in CatalogVariantSeed) (string, bool, error) {
	if p == nil || p.variants == nil {
		return "", false, fmt.Errorf("pluginsdk: catalog seed port: variants not configured")
	}
	productID := strings.TrimSpace(in.ProductID)
	if productID == "" {
		slug := strings.TrimSpace(in.ProductSlug)
		if slug == "" {
			return "", false, fmt.Errorf("pluginsdk: catalog seed port: variant product_id or product_slug must not be empty")
		}
		if p.products == nil {
			return "", false, fmt.Errorf("pluginsdk: catalog seed port: products not configured")
		}
		ctx = catalog.WithOperatorProductReadScope(ctx)
		prod, err := p.products.FindBySlug(ctx, slug)
		if err != nil {
			return "", false, err
		}
		if prod == nil {
			return "", false, fmt.Errorf("pluginsdk: catalog seed port: product slug %q not found", slug)
		}
		productID = prod.ID
	}
	sku := strings.TrimSpace(in.SKU)
	if sku == "" {
		return "", false, fmt.Errorf("pluginsdk: catalog seed port: variant sku must not be empty")
	}

	existing, err := p.variants.FindBySKU(ctx, sku)
	if err != nil {
		return "", false, err
	}
	if existing != nil {
		if existing.ProductID != productID {
			return "", false, fmt.Errorf("pluginsdk: catalog seed port: variant SKU %q belongs to product %q, expected %q",
				sku, existing.ProductID, productID)
		}
		return existing.ID, false, nil
	}

	v, err := catalog.NewVariant(id.New(), productID, sku)
	if err != nil {
		return "", false, err
	}
	v.Name = strings.TrimSpace(in.Name)

	if err := p.variants.Create(ctx, &v); err != nil {
		if apperror.Is(err, apperror.CodeConflict) {
			ret, err2 := p.variants.FindBySKU(ctx, sku)
			if err2 != nil {
				return "", false, err2
			}
			if ret == nil {
				return "", false, err
			}
			if ret.ProductID != productID {
				return "", false, fmt.Errorf("pluginsdk: catalog seed port: variant SKU %q belongs to product %q, expected %q",
					sku, ret.ProductID, productID)
			}
			return ret.ID, false, nil
		}
		return "", false, err
	}
	return v.ID, true, nil
}

func (p *catalogSeedPort) EnsurePrice(ctx context.Context, in CatalogPriceSeed) (bool, error) {
	if p == nil || p.prices == nil {
		return false, fmt.Errorf("pluginsdk: catalog seed port: prices not configured")
	}
	if p.history == nil {
		return false, fmt.Errorf("pluginsdk: catalog seed port: price history not configured")
	}
	variantID := strings.TrimSpace(in.VariantID)
	if variantID == "" {
		return false, fmt.Errorf("pluginsdk: catalog seed port: price variant_id must not be empty")
	}
	currency := strings.TrimSpace(in.Currency)
	if currency == "" {
		return false, fmt.Errorf("pluginsdk: catalog seed port: price currency must not be empty")
	}
	storeID := strings.TrimSpace(in.StoreID)
	existing, err := p.prices.FindByVariantCurrencyAndStore(ctx, variantID, currency, storeID)
	if err != nil {
		return false, err
	}
	if existing != nil {
		return false, nil
	}
	money, err := shared.NewMoney(in.Amount, currency)
	if err != nil {
		return false, fmt.Errorf("pluginsdk: catalog seed port: invalid money: %w", err)
	}
	price, err := pricing.NewPrice(id.New(), variantID, storeID, money)
	if err != nil {
		return false, err
	}
	snap, err := pricing.NewPriceSnapshot(id.New(), variantID, storeID, money)
	if err != nil {
		return false, err
	}

	// When already inside Import's transaction, repos are WithTx-bound and db is nil.
	if p.db != nil {
		if txPrices, ok := p.prices.(catalogSeedTxPrice); ok {
			if txHistory, ok := p.history.(catalogSeedTxHistory); ok {
				tx, err := p.db.BeginTx(ctx, nil)
				if err != nil {
					return false, fmt.Errorf("pluginsdk: catalog seed port: begin tx: %w", err)
				}
				if err := txPrices.WithTx(tx).Upsert(ctx, &price); err != nil {
					_ = tx.Rollback()
					return false, err
				}
				if err := txHistory.WithTx(tx).Record(ctx, &snap); err != nil {
					_ = tx.Rollback()
					return false, err
				}
				if err := tx.Commit(); err != nil {
					return false, fmt.Errorf("pluginsdk: catalog seed port: commit price+history: %w", err)
				}
				return true, nil
			}
		}
	}

	if err := p.prices.Upsert(ctx, &price); err != nil {
		return false, err
	}
	if err := p.history.Record(ctx, &snap); err != nil {
		return false, err
	}
	return true, nil
}

func (p *catalogSeedPort) SetStock(ctx context.Context, variantID string, qty int) error {
	if p == nil || p.stock == nil {
		return fmt.Errorf("pluginsdk: catalog seed port: stock not configured")
	}
	variantID = strings.TrimSpace(variantID)
	if variantID == "" {
		return fmt.Errorf("pluginsdk: catalog seed port: stock variant_id must not be empty")
	}
	entry, err := inventory.NewStockEntry(variantID, qty)
	if err != nil {
		return err
	}
	return p.stock.SetStock(ctx, &entry)
}

// NewOrdersPort adapts an order repository to OrderPort.
func NewOrdersPort(orders domainorder.OrderRepository) OrderPort {
	return &ordersPort{orders: orders}
}

type ordersPort struct {
	orders domainorder.OrderRepository
}

// FindByID returns an OrderView with ContactEmail blanked: the default
// read-only port is available to any plugin that declares Need(CapOrders),
// and PII should not ride along by default just because a plugin wanted
// order status/currency. There is currently no privileged accessor that
// returns the real address — add one (gated by an explicit, narrower
// capability) if a concrete integration needs it.
func (p *ordersPort) FindByID(ctx context.Context, id string) (*extapi.OrderView, error) {
	if p == nil || p.orders == nil {
		return nil, fmt.Errorf("pluginsdk: orders port: not configured")
	}
	o, err := p.orders.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if o == nil {
		return nil, nil
	}
	return &extapi.OrderView{
		ID:         o.ID,
		CustomerID: o.CustomerID,
		Status:     string(o.Status()),
		Currency:   o.Currency,
	}, nil
}

// NewCustomerPort adapts a customer repository to CustomerPort.
func NewCustomerPort(customers domaincustomer.CustomerRepository) CustomerPort {
	return &customerPort{customers: customers}
}

type customerPort struct {
	customers domaincustomer.CustomerRepository
}

// FindByID returns a CustomerView with Email blanked — see ordersPort.FindByID.
func (p *customerPort) FindByID(ctx context.Context, id string) (*extapi.CustomerView, error) {
	if p == nil || p.customers == nil {
		return nil, fmt.Errorf("pluginsdk: customer port: not configured")
	}
	c, err := p.customers.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, nil
	}
	return &extapi.CustomerView{ID: c.ID}, nil
}

// cartService is the cart application surface CartPort needs (keeps plugins off internal/application).
type cartService interface {
	AddItem(ctx context.Context, cartID, customerID, variantID string, quantity int, opts cartApp.AddItemOptions) (*domaincart.Cart, error)
	GetCart(ctx context.Context, cartID, customerID string) (*domaincart.Cart, error)
	CreateCart(ctx context.Context, customerID, currency string) (*domaincart.Cart, error)
	EnsureActiveCart(ctx context.Context, customerID, currency string) (*domaincart.Cart, error)
	DiscardEmptyGuestCart(ctx context.Context, cartID string) (discarded bool, err error)
	CartView(ctx context.Context, c *domaincart.Cart) (*extapi.CartView, error)
	FilterCartViewForPlugin(view *extapi.CartView, pluginName string) *extapi.CartView
	DeleteLineExtension(ctx context.Context, cartID, customerID, variantID, fieldCode string) error
	UpsertLineExtension(ctx context.Context, cartID, customerID, variantID string, inputs []domainext.ValueInput, updatedBy string) error
	ExtensionFieldOwner(code string) string
	ExtensionFieldPluginOnly(code string) bool
}

// NewCartPort adapts cart application Service to CartPort.
func NewCartPort(svc *cartApp.Service) CartPort {
	return &cartPort{svc: svc}
}

type cartPort struct {
	svc   cartService
	owner string // non-empty when returned from AsHostFor / Adapt
}

func (p *cartPort) withOwner(pluginName string) *cartPort {
	if p == nil {
		return nil
	}
	out := *p
	out.owner = pluginName
	return &out
}

func (p *cartPort) AddItem(ctx context.Context, cartID, customerID, variantID string, qty int, opts CartAddItemOpts) error {
	if p == nil || p.svc == nil {
		return fmt.Errorf("pluginsdk: cart port: not configured")
	}
	domainOpts := cartApp.AddItemOptions{UpdatedBy: opts.UpdatedBy}
	if len(opts.Extensions) > 0 {
		domainOpts.Extensions = make([]domainext.ValueInput, 0, len(opts.Extensions))
		for _, e := range opts.Extensions {
			code := strings.TrimSpace(e.Code)
			if p.owner != "" && p.svc.ExtensionFieldPluginOnly(code) {
				owner := p.svc.ExtensionFieldOwner(code)
				if owner == "" || owner != p.owner {
					return apperror.Forbidden("cannot add another plugin's line extension")
				}
			}
			domainOpts.Extensions = append(domainOpts.Extensions, domainext.ValueInput{
				FieldCode: code,
				Value:     e.Value,
			})
		}
	}
	_, err := p.svc.AddItem(ctx, cartID, customerID, variantID, qty, domainOpts)
	return err
}

func (p *cartPort) viewFor(ctx context.Context, c *domaincart.Cart) (*extapi.CartView, error) {
	view, err := p.svc.CartView(ctx, c)
	if err != nil {
		return nil, err
	}
	if p.owner != "" {
		return p.svc.FilterCartViewForPlugin(view, p.owner), nil
	}
	return view, nil
}

func (p *cartPort) Get(ctx context.Context, cartID, customerID string) (*extapi.CartView, error) {
	if p == nil || p.svc == nil {
		return nil, fmt.Errorf("pluginsdk: cart port: not configured")
	}
	c, err := p.svc.GetCart(ctx, cartID, customerID)
	if err != nil {
		return nil, err
	}
	return p.viewFor(ctx, c)
}

func (p *cartPort) EnsureActive(ctx context.Context, customerID, currency string) (*extapi.CartView, error) {
	if p == nil || p.svc == nil {
		return nil, fmt.Errorf("pluginsdk: cart port: not configured")
	}
	c, err := p.svc.EnsureActiveCart(ctx, customerID, currency)
	if err != nil {
		return nil, err
	}
	return p.viewFor(ctx, c)
}

func resolveStorefrontCurrency(ctx context.Context, currency string) string {
	currency = strings.TrimSpace(currency)
	if currency != "" {
		return currency
	}
	if s := store.FromContext(ctx); s != nil && strings.TrimSpace(s.Currency) != "" {
		return strings.TrimSpace(s.Currency)
	}
	return "EUR"
}

func (p *cartPort) EnsureStorefrontCart(ctx context.Context, w http.ResponseWriter, r *http.Request, currency string) (*extapi.CartView, error) {
	if p == nil || p.svc == nil {
		return nil, fmt.Errorf("pluginsdk: cart port: not configured")
	}
	currency = resolveStorefrontCurrency(ctx, currency)
	customerID := CustomerID(r)
	if customerID != "" {
		return p.EnsureActive(ctx, customerID, currency)
	}
	if cartID, ok := storefrontsession.CartIDFromRequest(r); ok {
		c, err := p.svc.GetCart(ctx, cartID, "")
		if err != nil {
			if apperror.Is(err, apperror.CodeNotFound) || apperror.Is(err, apperror.CodeForbidden) {
				// Stale cookie or claimed (customer) cart id: replace below.
			} else {
				// DB / timeout / other — do not orphan by rewriting the cookie.
				return nil, err
			}
		} else if c != nil && c.IsActive() {
			// GetCart("", guest) only succeeds for guest-owned carts.
			return p.viewFor(ctx, c)
		}
		// Non-active guest cart: replace below (no silent reuse).
	}
	c, err := p.svc.CreateCart(ctx, "", currency)
	if err != nil {
		return nil, err
	}
	storefrontsession.SetCartCookieSecure(w, c.ID, storefrontsession.CookieSecure(ctx, r))
	return p.viewFor(ctx, c)
}

func (p *cartPort) DiscardEmptyGuestCart(ctx context.Context, w http.ResponseWriter, r *http.Request, cartID string) error {
	if p == nil || p.svc == nil {
		return fmt.Errorf("pluginsdk: cart port: not configured")
	}
	cartID = strings.TrimSpace(cartID)
	if cartID == "" {
		return nil
	}
	discarded, err := p.svc.DiscardEmptyGuestCart(ctx, cartID)
	// Clear the cookie as soon as the row is gone — even if orphan purge failed —
	// so Ensure's Set-Cookie is not left pointing at a deleted cart.
	if discarded && w != nil {
		storefrontsession.ClearCartCookieSecure(w, storefrontsession.CookieSecure(ctx, r))
	}
	return err
}

func (p *cartPort) DeleteLineExtension(ctx context.Context, cartID, customerID, variantID, fieldCode string) error {
	if p == nil || p.svc == nil {
		return fmt.Errorf("pluginsdk: cart port: not configured")
	}
	fieldCode = strings.TrimSpace(fieldCode)
	if p.owner != "" && p.svc.ExtensionFieldPluginOnly(fieldCode) {
		owner := p.svc.ExtensionFieldOwner(fieldCode)
		if owner == "" || owner != p.owner {
			return apperror.Forbidden("cannot delete another plugin's line extension")
		}
	}
	return p.svc.DeleteLineExtension(ctx, cartID, customerID, variantID, fieldCode)
}

func (p *cartPort) UpsertLineExtension(ctx context.Context, cartID, customerID, variantID string, values []CartExtensionValue, updatedBy string) error {
	if p == nil || p.svc == nil {
		return fmt.Errorf("pluginsdk: cart port: not configured")
	}
	if len(values) == 0 {
		return apperror.Validation("extension values must not be empty")
	}
	inputs := make([]domainext.ValueInput, 0, len(values))
	for _, e := range values {
		code := strings.TrimSpace(e.Code)
		if p.owner != "" && p.svc.ExtensionFieldPluginOnly(code) {
			owner := p.svc.ExtensionFieldOwner(code)
			if owner == "" || owner != p.owner {
				return apperror.Forbidden("cannot upsert another plugin's line extension")
			}
		}
		inputs = append(inputs, domainext.ValueInput{FieldCode: code, Value: e.Value})
	}
	return p.svc.UpsertLineExtension(ctx, cartID, customerID, variantID, inputs, updatedBy)
}
