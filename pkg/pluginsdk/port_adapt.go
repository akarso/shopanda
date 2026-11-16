package pluginsdk

import (
	"context"
	"fmt"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	domaincart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domaincustomer "github.com/akarso/shopanda/internal/domain/customer"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	domainorder "github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/pkg/extapi"
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
	v, err := p.variants.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, nil
	}
	return &extapi.CatalogVariant{
		ID:        v.ID,
		ProductID: v.ProductID,
		SKU:       v.SKU,
		Name:      v.Name,
	}, nil
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
}

// NewCartPort adapts cart application Service to CartPort.
func NewCartPort(svc *cartApp.Service) CartPort {
	return &cartPort{svc: svc}
}

type cartPort struct {
	svc cartService
}

func (p *cartPort) AddItem(ctx context.Context, cartID, customerID, variantID string, qty int, opts CartAddItemOpts) error {
	if p == nil || p.svc == nil {
		return fmt.Errorf("pluginsdk: cart port: not configured")
	}
	domainOpts := cartApp.AddItemOptions{UpdatedBy: opts.UpdatedBy}
	if len(opts.Extensions) > 0 {
		domainOpts.Extensions = make([]domainext.ValueInput, 0, len(opts.Extensions))
		for _, e := range opts.Extensions {
			domainOpts.Extensions = append(domainOpts.Extensions, domainext.ValueInput{
				FieldCode: e.Code,
				Value:     e.Value,
			})
		}
	}
	_, err := p.svc.AddItem(ctx, cartID, customerID, variantID, qty, domainOpts)
	return err
}
