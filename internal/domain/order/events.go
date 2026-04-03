package order

const (
	EventOrderCreated   = "order.created"
	EventOrderConfirmed = "order.confirmed"
	EventOrderPaid      = "order.paid"
	EventOrderCancelled = "order.cancelled"
	EventOrderFailed    = "order.failed"
)

// OrderCreatedData is the event payload when an order is created.
type OrderCreatedData struct {
	OrderID    string `json:"order_id"`
	CustomerID string `json:"customer_id"`
	Currency   string `json:"currency"`
	ItemCount  int    `json:"item_count"`
}

// OrderStatusChangedData is the event payload for status transitions.
type OrderStatusChangedData struct {
	OrderID   string `json:"order_id"`
	OldStatus string `json:"old_status"`
	NewStatus string `json:"new_status"`
}
