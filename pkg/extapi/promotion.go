package extapi

import "context"

// PromotionPricingItem is a stable view of a pricing line item for promotion evaluators.
type PromotionPricingItem struct {
	VariantID  string
	Quantity   int
	UnitAmount int64
	TotalAmount int64
	Currency   string
}

// CatalogConditionHandler evaluates a catalog promotion condition for one line item.
type CatalogConditionHandler func(ctx context.Context, config []byte, item *PromotionPricingItem) (bool, error)

// CatalogActionHandler computes a catalog promotion discount for one line item.
type CatalogActionHandler func(ctx context.Context, config []byte, item *PromotionPricingItem) (discountAmount int64, err error)

// CartConditionHandler evaluates a cart promotion condition against the cart subtotal.
type CartConditionHandler func(ctx context.Context, config []byte, subtotalAmount int64, currency string) (bool, error)

// CartActionHandler computes a cart promotion discount from the cart subtotal.
type CartActionHandler func(ctx context.Context, config []byte, subtotalAmount int64, currency string) (discountAmount int64, err error)
