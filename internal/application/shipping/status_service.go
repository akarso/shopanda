package shipping

import (
	"context"
	"strings"

	domainShipping "github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// shipmentEventBus is the subset of *event.Bus used to publish shipment events.
type shipmentEventBus interface {
	Publish(ctx context.Context, evt event.Event) error
}

// StatusService applies domain-valid shipment transitions for integrations and admin.
type StatusService struct {
	shipments domainShipping.ShipmentRepository
	bus       shipmentEventBus
	log       logger.Logger
}

// NewStatusService returns a StatusService backed by shipments.
func NewStatusService(shipments domainShipping.ShipmentRepository) *StatusService {
	if shipments == nil {
		panic("shipping: status service repository must not be nil")
	}
	return &StatusService{shipments: shipments}
}

// WithEventBus publishes shipment.* after successful transitions. Nil skips publish.
func (s *StatusService) WithEventBus(bus shipmentEventBus) *StatusService {
	if s == nil {
		return nil
	}
	s.bus = bus
	return s
}

// WithLogger records publish failures after persist. Nil skips logging.
func (s *StatusService) WithLogger(log logger.Logger) *StatusService {
	if s == nil {
		return nil
	}
	s.log = log
	return s
}

// StatusResult is the outcome of a shipment status change.
type StatusResult struct {
	Shipment       *domainShipping.Shipment
	PreviousStatus domainShipping.ShippingStatus
	Changed        bool
}

// FindByOrderID returns the shipment for an order, or NotFound when none exists.
func (s *StatusService) FindByOrderID(ctx context.Context, orderID string) (*domainShipping.Shipment, error) {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return nil, apperror.Validation("order id must not be empty")
	}
	sh, err := s.shipments.FindByOrderID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if sh == nil {
		return nil, apperror.NotFound("shipment not found")
	}
	return sh, nil
}

// MarkShipped transitions pending → shipped with a tracking number.
// Already shipped with the same tracking is an idempotent no-op when provider_ref
// is omitted (empty) or matches the stored value. A different tracking, or a
// non-empty provider_ref that disagrees with storage, is rejected (amend not supported).
func (s *StatusService) MarkShipped(ctx context.Context, orderID, trackingNumber, providerRef string) (StatusResult, error) {
	trackingNumber = strings.TrimSpace(trackingNumber)
	providerRef = strings.TrimSpace(providerRef)
	if trackingNumber == "" {
		return StatusResult{}, apperror.Wrap(apperror.CodeValidation, ErrTrackingRequired.Error(), ErrTrackingRequired)
	}
	sh, err := s.FindByOrderID(ctx, orderID)
	if err != nil {
		return StatusResult{}, err
	}
	previous := sh.Status()
	if previous == domainShipping.StatusShipped {
		if sameShippedIdentity(sh.TrackingNumber, sh.ProviderRef, trackingNumber, providerRef) {
			return StatusResult{Shipment: sh, PreviousStatus: previous, Changed: false}, nil
		}
		return StatusResult{}, apperror.Wrap(apperror.CodeValidation, ErrTrackingAmendNotSupported.Error(), ErrTrackingAmendNotSupported)
	}
	return s.applyLoaded(ctx, sh, previous, domainShipping.StatusShipped, func(working *domainShipping.Shipment) error {
		return working.Ship(trackingNumber, providerRef)
	})
}

// sameShippedIdentity reports whether an inbound ship retry matches stored identity.
// Empty inbound providerRef means "leave as stored" so warehouse retries that omit
// optional fields remain idempotent.
func sameShippedIdentity(storedTracking, storedProviderRef, tracking, providerRef string) bool {
	if storedTracking != tracking {
		return false
	}
	if providerRef == "" {
		return true
	}
	return storedProviderRef == providerRef
}

// MarkDelivered transitions shipped → delivered.
func (s *StatusService) MarkDelivered(ctx context.Context, orderID string) (StatusResult, error) {
	return s.apply(ctx, orderID, domainShipping.StatusDelivered, func(sh *domainShipping.Shipment) error {
		return sh.Deliver()
	})
}

// Cancel transitions pending → cancelled. Cancelled shipments cannot be reopened
// (order_id is unique; there is no recreate API).
func (s *StatusService) Cancel(ctx context.Context, orderID string) (StatusResult, error) {
	return s.apply(ctx, orderID, domainShipping.StatusCancelled, func(sh *domainShipping.Shipment) error {
		return sh.Cancel()
	})
}

func (s *StatusService) apply(
	ctx context.Context,
	orderID string,
	next domainShipping.ShippingStatus,
	transition func(*domainShipping.Shipment) error,
) (StatusResult, error) {
	sh, err := s.FindByOrderID(ctx, orderID)
	if err != nil {
		return StatusResult{}, err
	}
	previous := sh.Status()
	if previous == next {
		return StatusResult{Shipment: sh, PreviousStatus: previous, Changed: false}, nil
	}
	return s.applyLoaded(ctx, sh, previous, next, transition)
}

func (s *StatusService) applyLoaded(
	ctx context.Context,
	sh *domainShipping.Shipment,
	previous, next domainShipping.ShippingStatus,
	transition func(*domainShipping.Shipment) error,
) (StatusResult, error) {
	prevUpdatedAt := sh.UpdatedAt
	working := *sh
	if err := transition(&working); err != nil {
		return StatusResult{Shipment: sh, PreviousStatus: previous, Changed: false}, apperror.Validation(err.Error())
	}
	if err := s.shipments.UpdateStatus(ctx, &working, prevUpdatedAt); err != nil {
		// Return the pre-transition shipment so callers never see a lied-about status.
		return StatusResult{Shipment: sh, PreviousStatus: previous, Changed: false}, err
	}
	s.publishStatus(ctx, &working, previous, next)
	return StatusResult{Shipment: &working, PreviousStatus: previous, Changed: true}, nil
}

func (s *StatusService) publishStatus(
	ctx context.Context,
	sh *domainShipping.Shipment,
	oldStatus, newStatus domainShipping.ShippingStatus,
) {
	if s == nil || s.bus == nil || sh == nil {
		return
	}
	var name string
	switch newStatus {
	case domainShipping.StatusShipped:
		name = domainShipping.EventShipmentShipped
	case domainShipping.StatusDelivered:
		name = domainShipping.EventShipmentDelivered
	case domainShipping.StatusCancelled:
		name = domainShipping.EventShipmentCancelled
	default:
		return
	}
	if err := s.bus.Publish(ctx, event.New(name, "shipping.status_service", domainShipping.ShipmentStatusChangedData{
		ShipmentID:     sh.ID,
		OrderID:        sh.OrderID,
		OldStatus:      oldStatus,
		NewStatus:      newStatus,
		TrackingNumber: sh.TrackingNumber,
		ProviderRef:    sh.ProviderRef,
	})); err != nil && s.log != nil {
		s.log.Error("shipping.status_service.publish_failed", err, map[string]interface{}{
			"event":       name,
			"shipment_id": sh.ID,
			"order_id":    sh.OrderID,
		})
	}
}
