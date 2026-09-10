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
	Quantity  int    `json:"quantity"`
}
