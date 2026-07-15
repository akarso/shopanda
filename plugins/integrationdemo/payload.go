package integrationdemo

import (
	"errors"
	"strings"
)

// OrderStatusPayload accepts flat JSON or a simplified SAP IDoc wrapper.
type OrderStatusPayload struct {
	OrderID     string           `json:"order_id"`
	Status      string           `json:"status"`
	ExternalRef string           `json:"external_ref,omitempty"`
	E1ORDSTAT   *IDocOrderStatus `json:"E1ORDSTAT,omitempty"`
}

// IDocOrderStatus is a simplified SAP IDoc order-status segment.
type IDocOrderStatus struct {
	VBELN   string `json:"VBELN"`
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
}

// Normalize extracts order id, target status, and external reference from the payload.
func (p OrderStatusPayload) Normalize() (orderID, status, externalRef string, err error) {
	if p.E1ORDSTAT != nil {
		orderID = strings.TrimSpace(p.E1ORDSTAT.OrderID)
		status = strings.TrimSpace(p.E1ORDSTAT.Status)
		externalRef = strings.TrimSpace(p.E1ORDSTAT.VBELN)
	} else {
		orderID = strings.TrimSpace(p.OrderID)
		status = strings.TrimSpace(p.Status)
		externalRef = strings.TrimSpace(p.ExternalRef)
	}
	if orderID == "" {
		return "", "", "", errors.New("order_id is required")
	}
	if status == "" {
		return "", "", "", errors.New("status is required")
	}
	return orderID, status, externalRef, nil
}
