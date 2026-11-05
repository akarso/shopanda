package shipping

import (
	"errors"
	"strings"

	domainShipping "github.com/akarso/shopanda/internal/domain/shipping"
)

// NormalizeStatus maps ERP/admin aliases to domain shipment statuses.
func NormalizeStatus(raw string) (domainShipping.ShippingStatus, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("empty status")
	}
	switch strings.ToUpper(raw) {
	case "SHIPPED", "SHIP":
		return domainShipping.StatusShipped, nil
	case "DELIVERED", "DELIVERY":
		return domainShipping.StatusDelivered, nil
	case "CANCELLED", "CANCELED":
		return domainShipping.StatusCancelled, nil
	}
	status := domainShipping.ShippingStatus(strings.ToLower(raw))
	switch status {
	case domainShipping.StatusShipped, domainShipping.StatusDelivered, domainShipping.StatusCancelled:
		return status, nil
	}
	return "", errors.New("invalid status")
}
