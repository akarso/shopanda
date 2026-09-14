package inventory

// EventStockUpdated fires when a variant's on-hand stock quantity changes.
// PR-1036's search-index subscriber uses it to keep a product's indexed
// availability current without a manual reindex.
const EventStockUpdated = "inventory.stock.updated"

// StockUpdatedData is the payload for EventStockUpdated.
type StockUpdatedData struct {
	ProductID string `json:"product_id"`
	VariantID string `json:"variant_id"`
	SKU       string `json:"sku"`

	// OnHand is the resulting on-hand quantity after the change, when the
	// producer already has that total (admin Adjust, returns restocking).
	// Nil means this producer did not supply an absolute — do not treat a
	// missing OnHand as zero stock.
	OnHand *int `json:"on_hand,omitempty"`

	// Delta is the signed size of the change (negative = reserved /
	// decremented, positive = restored / incremented), when the producer
	// knows the change size (checkout reserve/release, reservation-expiry
	// restore). Nil means this producer did not supply a delta.
	Delta *int `json:"delta,omitempty"`
}

// Qty returns a pointer to n for optional StockUpdatedData.OnHand / Delta.
func Qty(n int) *int { return &n }
