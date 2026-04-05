package shipping

// Event names for shipping domain.
const (
	EventShipmentCreated   = "shipment.created"
	EventShipmentShipped   = "shipment.shipped"
	EventShipmentDelivered = "shipment.delivered"
	EventShipmentCancelled = "shipment.cancelled"
)

// ShipmentCreatedData is emitted when a new shipment is created.
type ShipmentCreatedData struct {
	ShipmentID string         `json:"shipment_id"`
	OrderID    string         `json:"order_id"`
	Method     ShippingMethod `json:"method"`
	Cost       int64          `json:"cost"`
	Currency   string         `json:"currency"`
}

// ShipmentStatusChangedData is emitted on status transitions.
type ShipmentStatusChangedData struct {
	ShipmentID     string         `json:"shipment_id"`
	OrderID        string         `json:"order_id"`
	OldStatus      ShippingStatus `json:"old_status"`
	NewStatus      ShippingStatus `json:"new_status"`
	TrackingNumber string         `json:"tracking_number,omitempty"`
	ProviderRef    string         `json:"provider_ref,omitempty"`
}
