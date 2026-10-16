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

// ParseCartItemTargetID splits a cart_item target_id on the first ':'.
func ParseCartItemTargetID(targetID string) (cartID, variantID string, ok bool) {
	targetID = strings.TrimSpace(targetID)
	i := strings.IndexByte(targetID, ':')
	if i <= 0 || i >= len(targetID)-1 {
		return "", "", false
	}
	cartID = strings.TrimSpace(targetID[:i])
	variantID = strings.TrimSpace(targetID[i+1:])
	if cartID == "" || variantID == "" {
		return "", "", false
	}
	return cartID, variantID, true
}
