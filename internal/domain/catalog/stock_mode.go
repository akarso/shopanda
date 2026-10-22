package catalog

import (
	"errors"
	"fmt"
	"strings"
)

// StockMode chooses how a product's available quantity is resolved.
// fixed reads the product's own stock row. dynamic (bundle only) is the
// minimum of floor(component stock / required quantity) across required
// components at resolution time and does not write that quantity.
// Virtual and downloadable required leaves do not gate that minimum.
// A simple leaf still needs a stock row.
type StockMode string

const (
	StockModeFixed   StockMode = "fixed"
	StockModeDynamic StockMode = "dynamic"
)

// DynamicStockModeBundleOnlyMessage is the validation text when dynamic is
// sent for a product that is not a bundle.
const DynamicStockModeBundleOnlyMessage = "stock mode dynamic is only allowed for bundle products"

// DynamicBundleOwnStockMessage is the validation text when a product is set
// to dynamic stock while one of its variants still has stock.quantity > 0.
const DynamicBundleOwnStockMessage = "remove bundle stock before enabling dynamic stock"

// DynamicBundleStockRejectedMessage is the validation text when a caller tries
// to store stock on a dynamic bundle. The sellable quantity is the component
// rollup; an own row would disagree with search and the cart cap.
const DynamicBundleStockRejectedMessage = "dynamic bundle products cannot have their own stock"

// BundleCannotBeReservedMessage is the customer-facing text when a dynamic
// bundle cannot be priced, quantity-checked, or reserved because a required
// leaf failed. Callers keep the leaf or component detail on Unwrap; HTTP
// responses use this message only.
const BundleCannotBeReservedMessage = "bundle cannot be reserved"

// MaxDigitalBundleLineQty is the sellable line cap when every required leaf
// is virtual or downloadable. Those leaves have no stock row, so the floor
// has nothing to gate on. The cap is finite so a cart cannot add an unbounded
// quantity (anti-abuse). It is not a stock level and it is not unlimited.
// Mixed bundles that include a simple leaf keep min(floor(stock / quantity))
// and do not use this cap. Listing in-stock stays the boolean "at least one
// kit": this cap is positive, so a digital-only kit is in stock, and cart
// add uses the same number from ResolveAvailableQty.
const MaxDigitalBundleLineQty = 100

// AllStockModes returns every recognised stock mode in stable order.
// Keep migrations/092_add_products_stock_mode.sql CHECK in sync.
func AllStockModes() []StockMode {
	return []StockMode{StockModeFixed, StockModeDynamic}
}

// IsValid returns true if m is fixed or dynamic.
func (m StockMode) IsValid() bool {
	for _, allowed := range AllStockModes() {
		if m == allowed {
			return true
		}
	}
	return false
}

// InvalidStockModeMessage is the canonical validation text for an unknown mode.
func InvalidStockModeMessage(m StockMode) string {
	return fmt.Sprintf("invalid stock mode %q", m)
}

// ApplyStockMode stores mode on a bundle. present is false when the caller
// omitted the field. An explicit dynamic token on any other type is rejected.
// An omitted mode is coerced to fixed only when the product is not a bundle
// (including a type change off bundle). A bundle that omits the field keeps
// its current mode. An unrecognised token is rejected for every type.
func (p *Product) ApplyStockMode(raw string, present bool) error {
	if p == nil {
		return errors.New("product must not be nil")
	}
	if present {
		mode := StockMode(strings.TrimSpace(raw))
		if !mode.IsValid() {
			return errors.New(InvalidStockModeMessage(mode))
		}
		if mode == StockModeDynamic && p.Type != TypeBundle {
			return errors.New(DynamicStockModeBundleOnlyMessage)
		}
		p.StockMode = mode
		return nil
	}
	if p.Type != TypeBundle || !p.StockMode.IsValid() {
		p.StockMode = StockModeFixed
	}
	return nil
}
