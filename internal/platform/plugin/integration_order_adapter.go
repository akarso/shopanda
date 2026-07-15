package plugin

import (
	"context"
	"errors"
	"strings"

	orderApp "github.com/akarso/shopanda/internal/application/order"
	domainOrder "github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/pkg/extapi"
)

// NewIntegrationOrderStatusUpdater adapts the core order status service for integration plugins.
func NewIntegrationOrderStatusUpdater(svc *orderApp.StatusService) extapi.IntegrationOrderStatusUpdater {
	if svc == nil {
		panic("plugin: integration order status service must not be nil")
	}
	return &integrationOrderStatusAdapter{svc: svc}
}

type integrationOrderStatusAdapter struct {
	svc *orderApp.StatusService
}

func (a *integrationOrderStatusAdapter) ApplyOrderStatus(ctx context.Context, orderID, status string) (extapi.IntegrationOrderStatusResult, error) {
	orderID = strings.TrimSpace(orderID)
	target, err := normalizeIntegrationOrderStatus(status)
	if err != nil {
		return extapi.IntegrationOrderStatusResult{}, extapi.ErrIntegrationOrderInvalidStatus
	}

	o, previous, changed, err := a.svc.ApplyStatus(ctx, orderID, target)
	if err != nil {
		return extapi.IntegrationOrderStatusResult{}, mapIntegrationOrderStatusError(err)
	}

	result := extapi.IntegrationOrderStatusResult{
		OrderID:        o.ID,
		Status:         string(o.Status()),
		PreviousStatus: string(previous),
		Changed:        changed,
	}
	return result, nil
}

func normalizeIntegrationOrderStatus(raw string) (domainOrder.OrderStatus, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("empty status")
	}
	switch strings.ToUpper(raw) {
	case "CONFIRMED", "CR":
		return domainOrder.OrderStatusConfirmed, nil
	case "PAID", "SETTLED":
		return domainOrder.OrderStatusPaid, nil
	case "CANCELLED", "CANCELED":
		return domainOrder.OrderStatusCancelled, nil
	case "FAILED":
		return domainOrder.OrderStatusFailed, nil
	}
	status := domainOrder.OrderStatus(strings.ToLower(raw))
	if status.IsValid() {
		return status, nil
	}
	return "", errors.New("invalid status")
}

func mapIntegrationOrderStatusError(err error) error {
	var appErr *apperror.Error
	if errors.As(err, &appErr) {
		switch appErr.Code {
		case apperror.CodeNotFound:
			return extapi.ErrIntegrationOrderNotFound
		case apperror.CodeValidation:
			return extapi.ErrIntegrationOrderInvalidTransition
		}
	}
	return err
}
