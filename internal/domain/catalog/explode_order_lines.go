package catalog

import "errors"

// ExplodeOrderLinesRequiresDynamicMessage is the validation text when
// explode_order_lines is true on a product that is not a bundle with both
// pricing_mode and stock_mode dynamic. A fixed price cannot be split into
// component catalog prices, and fixed stock decrements the kit SKU, so an
// itemized return would restock the wrong identity.
const ExplodeOrderLinesRequiresDynamicMessage = "explode order lines is only allowed for a bundle with dynamic pricing and dynamic stock"

// ApplyExplodeOrderLines stores the flag. present is false when the caller
// omitted the field. true is rejected unless the product is a bundle with
// dynamic pricing and dynamic stock. An omitted flag is coerced to false
// whenever the product is not allowed to explode: a type change off bundle,
// or a pricing/stock mode change that leaves the bundle ineligible. A bundle
// that still has both modes dynamic and omits the field keeps its current
// flag. Call this after type and both modes are applied.
func (p *Product) ApplyExplodeOrderLines(value bool, present bool) error {
	if p == nil {
		return errors.New("product must not be nil")
	}
	if present {
		if value && !p.explodeOrderLinesAllowed() {
			return errors.New(ExplodeOrderLinesRequiresDynamicMessage)
		}
		p.ExplodeOrderLines = value
		return nil
	}
	if !p.explodeOrderLinesAllowed() {
		p.ExplodeOrderLines = false
	}
	return nil
}

func (p *Product) explodeOrderLinesAllowed() bool {
	return p.Type == TypeBundle &&
		p.PricingMode == PricingModeDynamic &&
		p.StockMode == StockModeDynamic
}
