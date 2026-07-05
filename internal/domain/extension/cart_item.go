package extension

import "strings"

// CartItemTargetID returns the stable target_id for a cart line extension value.
func CartItemTargetID(cartID, variantID string) string {
	return strings.TrimSpace(cartID) + ":" + strings.TrimSpace(variantID)
}

// CartItemTarget builds a cart_item extension target for cartID and variantID.
func CartItemTarget(cartID, variantID string) Target {
	return Target{
		Type: TargetCartItem,
		ID:   CartItemTargetID(cartID, variantID),
	}
}
