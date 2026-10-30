package download

import (
	"github.com/akarso/shopanda/internal/domain/assignment"
)

// OrderItemID mints the opaque id for an order line grant. Delegates to
// assignment.UnitID (orderID + "/" + variantID) so grants, assignments, and
// returns share one scheme. Both parts are trimmed. An empty part yields "".
func OrderItemID(orderID, variantID string) string {
	return assignment.UnitID(orderID, variantID)
}

// SplitOrderItemID parses an id minted by OrderItemID.
func SplitOrderItemID(orderItemID string) (orderID, variantID string, ok bool) {
	return assignment.SplitUnitID(orderItemID)
}

// CanonicalOrderItemID rebuilds an order-item id after SplitOrderItemID.
// The order part must be a UUID and is lowercased. The variant part is
// left as written.
func CanonicalOrderItemID(orderItemID string) (string, bool) {
	return assignment.CanonicalUnitID(orderItemID)
}
