package inventory

import "time"

// InventoryListItem is an admin inventory row enriched with catalog labels.
type InventoryListItem struct {
	VariantID   string
	ProductID   string
	SKU         string
	ProductName string
	VariantName string
	Quantity    int
	Reserved    int
	UpdatedAt   time.Time
}

// Available returns on-hand quantity minus active reservations.
func (i InventoryListItem) Available() int {
	available := i.Quantity - i.Reserved
	if available < 0 {
		return 0
	}
	return available
}
