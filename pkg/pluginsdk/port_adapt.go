package pluginsdk

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	domaincart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domaincustomer "github.com/akarso/shopanda/internal/domain/customer"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	domainorder "github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/platform/apperror"
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
			cv.ProductSlug = prod.Slug
		}
		// Empty ProductSlug with nil error: parent missing, non-active under
		// storefront read scope, empty slug column, or products repo nil.
		// Hooks must treat empty as fail-closed for "belongs to slug X", not skip.
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
