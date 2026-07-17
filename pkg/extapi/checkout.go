package extapi

// Canonical core checkout workflow step names (stable v0).
const (
	CheckoutStepValidateCart       = "validate_cart"
	CheckoutStepRecalculatePricing = "recalculate_pricing"
	CheckoutStepReserveInventory   = "reserve_inventory"
	CheckoutStepCreateOrder        = "create_order"
	CheckoutStepSelectShipping     = "select_shipping"
	CheckoutStepInitiatePayment    = "initiate_payment"
)

// Checkout position shortcuts (stable v0).
const (
	CheckoutPositionStart = "start"
	CheckoutPositionEnd   = "end"
)

var checkoutStepCatalog = []string{
	CheckoutStepValidateCart,
	CheckoutStepRecalculatePricing,
	CheckoutStepReserveInventory,
	CheckoutStepCreateOrder,
	CheckoutStepSelectShipping,
	CheckoutStepInitiatePayment,
}

// CheckoutStepCatalog returns documented core checkout step names.
func CheckoutStepCatalog() []string {
	out := make([]string, len(checkoutStepCatalog))
	copy(out, checkoutStepCatalog)
	return out
}

// AfterCheckoutStep returns an after-position for a core step name or alias.
func AfterCheckoutStep(anchor string) string {
	return "after:" + anchor
}

// BeforeCheckoutStep returns a before-position for a core step name or alias.
func BeforeCheckoutStep(anchor string) string {
	return "before:" + anchor
}
