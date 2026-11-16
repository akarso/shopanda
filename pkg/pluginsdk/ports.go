package pluginsdk

import (
	"context"

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

// CartPort adds lines to a cart with pricing, hooks, and optional extensions.
type CartPort interface {
	AddItem(ctx context.Context, cartID, customerID, variantID string, qty int, opts CartAddItemOpts) error
}

// CatalogPort provides minimal catalog reads for seed and integration plugins.
type CatalogPort interface {
	FindVariant(ctx context.Context, id string) (*extapi.CatalogVariant, error)
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
