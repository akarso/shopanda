package taxdemo

const (
	// DefaultFlatRateBPS is the default flat VAT rate when config is unset (19%).
	DefaultFlatRateBPS = 1900
	// TaxStepName is the pricing pipeline step identifier after replace:tax.
	TaxStepName = "taxdemo.flat_tax"
	// AdjustmentCode is the tax adjustment code applied to line items.
	AdjustmentCode = "taxdemo.flat"
)
