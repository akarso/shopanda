package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	shippingapp "github.com/akarso/shopanda/internal/application/shipping"
	domainShipping "github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/pkg/extapi"
)

// NewIntegrationShipmentStatusUpdater adapts the core shipment status service for integration plugins.
func NewIntegrationShipmentStatusUpdater(svc *shippingapp.StatusService) extapi.IntegrationShipmentStatusUpdater {
	if svc == nil {
		panic("plugin: integration shipment status service must not be nil")
	}
	return &integrationShipmentStatusAdapter{svc: svc}
}

type integrationShipmentStatusAdapter struct {
	svc *shippingapp.StatusService
}

func (a *integrationShipmentStatusAdapter) ApplyShipmentStatus(
	ctx context.Context,
	orderID, status, trackingNumber, providerRef string,
) (extapi.IntegrationShipmentStatusResult, error) {
	orderID = strings.TrimSpace(orderID)
	target, err := shippingapp.NormalizeStatus(status)
	if err != nil {
		return extapi.IntegrationShipmentStatusResult{}, extapi.ErrIntegrationShipmentInvalidStatus
	}

	var res shippingapp.StatusResult
	switch target {
	case domainShipping.StatusShipped:
		res, err = a.svc.MarkShipped(ctx, orderID, trackingNumber, providerRef)
	case domainShipping.StatusDelivered:
		res, err = a.svc.MarkDelivered(ctx, orderID)
	case domainShipping.StatusCancelled:
		res, err = a.svc.Cancel(ctx, orderID)
	default:
		return extapi.IntegrationShipmentStatusResult{}, extapi.ErrIntegrationShipmentInvalidStatus
	}
	if err != nil {
		return extapi.IntegrationShipmentStatusResult{}, mapIntegrationShipmentStatusError(err)
	}

	return extapi.IntegrationShipmentStatusResult{
		OrderID:        res.Shipment.OrderID,
		ShipmentID:     res.Shipment.ID,
		Status:         string(res.Shipment.Status()),
		PreviousStatus: string(res.PreviousStatus),
		Changed:        res.Changed,
		TrackingNumber: res.Shipment.TrackingNumber,
		ProviderRef:    res.Shipment.ProviderRef,
	}, nil
}

func mapIntegrationShipmentStatusError(err error) error {
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		return err
	}
	switch appErr.Code {
	case apperror.CodeNotFound:
		return extapi.ErrIntegrationShipmentNotFound
	case apperror.CodeConflict:
		return fmt.Errorf("%w: %s", extapi.ErrIntegrationShipmentConflict, appErr.Message)
	case apperror.CodeValidation:
		msg := appErr.Message
		if errors.Is(err, shippingapp.ErrTrackingRequired) || errors.Is(err, shippingapp.ErrOrderIDRequired) {
			return fmt.Errorf("%w: %s", extapi.ErrIntegrationShipmentInvalidPayload, msg)
		}
		// Includes ErrTrackingAmendNotSupported, ErrOrderNotFulfillable, domain lifecycle refusals.
		return fmt.Errorf("%w: %s", extapi.ErrIntegrationShipmentInvalidTransition, msg)
	}
	return err
}
