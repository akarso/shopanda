package integrationdemo

import (
	"errors"
	"strings"
)

// ShipmentStatusPayload is the inbound warehouse/ERP shipment status body.
type ShipmentStatusPayload struct {
	OrderID        string `json:"order_id"`
	Status         string `json:"status"`
	TrackingNumber string `json:"tracking_number,omitempty"`
	ProviderRef    string `json:"provider_ref,omitempty"`
	ExternalRef    string `json:"external_ref,omitempty"`
}

// Normalize extracts order id, status, tracking, provider ref, and external reference.
func (p ShipmentStatusPayload) Normalize() (orderID, status, tracking, providerRef, externalRef string, err error) {
	orderID = strings.TrimSpace(p.OrderID)
	status = strings.TrimSpace(p.Status)
	tracking = strings.TrimSpace(p.TrackingNumber)
	providerRef = strings.TrimSpace(p.ProviderRef)
	externalRef = strings.TrimSpace(p.ExternalRef)
	if orderID == "" {
		return "", "", "", "", "", errors.New("order_id is required")
	}
	if status == "" {
		return "", "", "", "", "", errors.New("status is required")
	}
	return orderID, status, tracking, providerRef, externalRef, nil
}
