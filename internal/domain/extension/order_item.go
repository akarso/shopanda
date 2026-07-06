package extension

import "strings"

// OrderItemTargetID returns the stable target_id for an order line extension value.
func OrderItemTargetID(orderID, variantID string) string {
	return strings.TrimSpace(orderID) + ":" + strings.TrimSpace(variantID)
}

// OrderItemTarget builds an order_item extension target for orderID and variantID.
func OrderItemTarget(orderID, variantID string) Target {
	return Target{
		Type: TargetOrderItem,
		ID:   OrderItemTargetID(orderID, variantID),
	}
}
