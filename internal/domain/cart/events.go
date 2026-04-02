package cart

const (
	EventItemAdded   = "cart.item.added"
	EventItemUpdated = "cart.item.updated"
	EventItemRemoved = "cart.item.removed"
)

// ItemAddedData is the event payload when an item is added.
type ItemAddedData struct {
	CartID    string `json:"cart_id"`
	VariantID string `json:"variant_id"`
	Quantity  int    `json:"quantity"`
}

// ItemUpdatedData is the event payload when an item quantity is updated.
type ItemUpdatedData struct {
	CartID    string `json:"cart_id"`
	VariantID string `json:"variant_id"`
	Quantity  int    `json:"quantity"`
}

// ItemRemovedData is the event payload when an item is removed.
type ItemRemovedData struct {
	CartID    string `json:"cart_id"`
	VariantID string `json:"variant_id"`
}
