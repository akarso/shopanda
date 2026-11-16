package extapi

import "context"

// AdjustmentType classifies a price adjustment.
type AdjustmentType string

const (
	AdjustmentDiscount AdjustmentType = "discount"
	AdjustmentTax      AdjustmentType = "tax"
	AdjustmentFee      AdjustmentType = "fee"
)

// PricingAdjustment is a public price adjustment (fee, tax, discount).
type PricingAdjustment struct {
	Type        AdjustmentType
	Code        string
	Description string
	Amount      Money
	Included    bool
	Meta        map[string]interface{}
}

// PricingLine is one cart line in a pricing pass.
type PricingLine struct {
	VariantID string
	Quantity  int
	UnitPrice Money
	Total     Money
	// Adjustments are the line-level adjustments applied so far via
	// AddItemAdjustment (e.g. a per-line tax or discount). Cart-level
	// adjustments (AddAdjustment) are not included here — see Adjustments().
	Adjustments []PricingAdjustment
}

// PricingContext is the mutable view plugins use in pricing steps.
// Core adapts this to the domain pipeline context.
type PricingContext interface {
	Currency() string
	// Items returns the current cart lines, including any line-level
	// adjustments already applied by earlier steps (see PricingLine.Adjustments).
	Items() []PricingLine
	// Adjustments returns cart-level price adjustments applied so far via
	// AddAdjustment (not line-level adjustments — see Items()).
	Adjustments() []PricingAdjustment
	AddAdjustment(adj PricingAdjustment) error
	// AddItemAdjustment appends adj to the line at index (as returned by
	// Items()), contributing to pipeline totals (e.g. TaxTotal) without
	// replacing the cart-level Adjustments slice. index must be in range.
	AddItemAdjustment(index int, adj PricingAdjustment) error
	// SetItemPrice replaces the unit price and total for the line at index
	// (e.g. a group/tier price override, or an inclusive-tax total rebase).
	// Pass the line's current UnitPrice to leave it unchanged.
	SetItemPrice(index int, unitPrice, total Money) error

	// Subtotal, Discounts, Tax, Fees, and GrandTotal return the pipeline's
	// running totals as computed so far. These are populated by the core
	// finalize step, which typically runs last — a step registered before
	// finalize sees zero-valued totals (in this context's Currency) here;
	// register "after:finalize" (where supported) or recompute from Items()
	// if a step needs the running total mid-pipeline.
	Subtotal() Money
	Discounts() Money
	Tax() Money
	Fees() Money
	GrandTotal() Money

	MetaGet(key string) (interface{}, bool)
	MetaSet(key string, value interface{})
}

// PricingStep is the public pricing pipeline step contract.
type PricingStep interface {
	Name() string
	Apply(ctx context.Context, pctx PricingContext) error
}
