package extapi

import (
	"context"
	"errors"
)

// Integration shipment status error codes for structured error responses.
const (
	IntegrationErrorShipmentNotFound          = "shipment.not_found"
	IntegrationErrorShipmentInvalidStatus     = "shipment.invalid_status"
	IntegrationErrorShipmentInvalidPayload    = "shipment.invalid_payload"
	IntegrationErrorShipmentInvalidTransition = "shipment.invalid_transition"
	IntegrationErrorShipmentConflict          = "shipment.conflict"
)

var (
	// ErrIntegrationShipmentNotFound indicates no shipment exists for the order.
	ErrIntegrationShipmentNotFound = errors.New("integration shipment not found")
	// ErrIntegrationShipmentInvalidStatus indicates the requested status is not recognized.
	ErrIntegrationShipmentInvalidStatus = errors.New("integration shipment invalid status")
	// ErrIntegrationShipmentInvalidPayload indicates bad input (e.g. missing tracking on ship).
	ErrIntegrationShipmentInvalidPayload = errors.New("integration shipment invalid payload")
	// ErrIntegrationShipmentInvalidTransition indicates the shipment cannot move to the requested status.
	ErrIntegrationShipmentInvalidTransition = errors.New("integration shipment invalid transition")
	// ErrIntegrationShipmentConflict indicates an optimistic-lock / concurrent update conflict.
	ErrIntegrationShipmentConflict = errors.New("integration shipment conflict")
)

// IntegrationShipmentStatusResult is the outcome of an inbound shipment status update.
type IntegrationShipmentStatusResult struct {
	OrderID        string `json:"order_id"`
	ShipmentID     string `json:"shipment_id"`
	Status         string `json:"status"`
	PreviousStatus string `json:"previous_status,omitempty"`
	Changed        bool   `json:"changed"`
	TrackingNumber string `json:"tracking_number,omitempty"`
	ProviderRef    string `json:"provider_ref,omitempty"`
	ExternalRef    string `json:"external_ref,omitempty"`
}

// IntegrationShipmentStatusUpdater applies inbound warehouse/ERP shipment status updates.
// Implemented by core and exposed to integration plugins via plugin.App.
type IntegrationShipmentStatusUpdater interface {
	ApplyShipmentStatus(ctx context.Context, orderID, status, trackingNumber, providerRef string) (IntegrationShipmentStatusResult, error)
}
