package extension

// TargetType identifies where an extension field applies (entity or view context).
type TargetType string

const (
	TargetProduct    TargetType = "product"
	TargetVariant    TargetType = "variant"
	TargetCartItem   TargetType = "cart_item"
	TargetOrderItem  TargetType = "order_item"
	TargetCustomer   TargetType = "customer"
	TargetPDP        TargetType = "pdp"
	TargetPLPItem    TargetType = "plp_item"
	TargetCartView   TargetType = "cart_view"
	TargetCheckoutView TargetType = "checkout_view"
)

// IsEntity reports whether t is a persisted entity target.
func (t TargetType) IsEntity() bool {
	switch t {
	case TargetProduct, TargetVariant, TargetCartItem, TargetOrderItem, TargetCustomer:
		return true
	}
	return false
}

// IsContext reports whether t is a computed view-context target.
func (t TargetType) IsContext() bool {
	switch t {
	case TargetPDP, TargetPLPItem, TargetCartView, TargetCheckoutView:
		return true
	}
	return false
}

// IsValid reports whether t is a recognised target type.
func (t TargetType) IsValid() bool {
	return t.IsEntity() || t.IsContext()
}
