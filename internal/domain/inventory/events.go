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

	// Quantity's meaning is NOT consistent across publish sites — it is
	// either the resulting on-hand total after the change (admin Adjust,
	// returns restocking, both of which already have that value in hand)
	// or the size of the change itself (checkout reserve/release, the
	// reservation-expiry sweep — an absolute total there would cost an
	// extra query neither site otherwise needs). This is only harmless
	// today because the sole consumer (search's HandleStockUpdated) reads
	// only ProductID and ignores Quantity entirely. A future consumer
	// that needs a precise, consistently-shaped quantity must query
	// current stock itself — do not trust this field's shape without
	// checking the specific publish site it came from.
	Quantity int `json:"quantity"`
}
