package extapi

import (
	"context"
	"errors"
)

// Integration order status error codes for structured error responses.
const (
	IntegrationErrorOrderNotFound          = "order.not_found"
	IntegrationErrorOrderInvalidStatus     = "order.invalid_status"
	IntegrationErrorOrderInvalidTransition = "order.invalid_transition"
)

var (
	// ErrIntegrationOrderNotFound indicates the target order does not exist.
	ErrIntegrationOrderNotFound = errors.New("integration order not found")
	// ErrIntegrationOrderInvalidStatus indicates the requested status is not recognized.
	ErrIntegrationOrderInvalidStatus = errors.New("integration order invalid status")
	// ErrIntegrationOrderInvalidTransition indicates the order cannot move to the requested status.
	ErrIntegrationOrderInvalidTransition = errors.New("integration order invalid transition")
)

// IntegrationOrderStatusResult is the outcome of an inbound ERP order status update.
type IntegrationOrderStatusResult struct {
	OrderID        string `json:"order_id"`
	Status         string `json:"status"`
	PreviousStatus string `json:"previous_status,omitempty"`
	Changed        bool   `json:"changed"`
	ExternalRef    string `json:"external_ref,omitempty"`
}

// IntegrationOrderStatusUpdater applies inbound ERP order status updates.
// Implemented by core and exposed to integration plugins via plugin.App.
type IntegrationOrderStatusUpdater interface {
	ApplyOrderStatus(ctx context.Context, orderID, status string) (IntegrationOrderStatusResult, error)
}
