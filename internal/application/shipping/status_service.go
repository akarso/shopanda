package shipping

import (
	"context"
	"errors"
	"strings"

	domainOrder "github.com/akarso/shopanda/internal/domain/order"
	domainShipping "github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// shipmentEventBus is the subset of *event.Bus used to publish shipment events.
type shipmentEventBus interface {
	Publish(ctx context.Context, evt event.Event) error
}

// orderLookup loads orders so ship/deliver can refuse cancelled/failed fulfillment.
type orderLookup interface {
	FindByID(ctx context.Context, id string) (*domainOrder.Order, error)
}

// StatusService applies domain-valid shipment transitions for integrations and admin.
type StatusService struct {
	shipments domainShipping.ShipmentRepository
	orders    orderLookup
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

// WithOrders gates ship/deliver on cancelled/failed order status. Nil skips the check
// (tests that only cover shipment transitions); production wiring always sets this.
func (s *StatusService) WithOrders(orders orderLookup) *StatusService {
	if s == nil {
		return nil
	}
	s.orders = orders
	return s
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
		return nil, apperror.Wrap(apperror.CodeValidation, ErrOrderIDRequired.Error(), ErrOrderIDRequired)
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
// is omitted (empty) or matches the stored value; the shipment.shipped event is
// republished so a lost async email side effect can heal (deduped by notification).
// A different tracking, or a non-empty provider_ref that disagrees with storage,
// is rejected (amend not supported). Cancelled/failed orders cannot be shipped.
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
		if !sameShippedIdentity(sh.TrackingNumber, sh.ProviderRef, trackingNumber, providerRef) {
			return StatusResult{}, apperror.Wrap(apperror.CodeValidation, ErrTrackingAmendNotSupported.Error(), ErrTrackingAmendNotSupported)
		}
		// Heal republish only while the order is still fulfillable.
		// Cancelled/failed → silent no-heal (shipment already shipped; do not fail ERP retry).
		// Other lookup errors must propagate so callers can retry heal.
		if err := s.requireFulfillableOrder(ctx, orderID); err != nil {
			if errors.Is(err, ErrOrderNotFulfillable) {
				return StatusResult{Shipment: sh, PreviousStatus: previous, Changed: false}, nil
			}
			return StatusResult{Shipment: sh, PreviousStatus: previous, Changed: false}, err
		}
		s.publishStatus(ctx, sh, previous, domainShipping.StatusShipped)
		return StatusResult{Shipment: sh, PreviousStatus: previous, Changed: false}, nil
	}
	if err := s.requireFulfillableOrder(ctx, orderID); err != nil {
		return StatusResult{}, err
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
// Cancelled/failed orders cannot be marked delivered.
func (s *StatusService) MarkDelivered(ctx context.Context, orderID string) (StatusResult, error) {
	sh, err := s.FindByOrderID(ctx, orderID)
	if err != nil {
		return StatusResult{}, err
	}
	previous := sh.Status()
	if previous == domainShipping.StatusDelivered {
		return StatusResult{Shipment: sh, PreviousStatus: previous, Changed: false}, nil
	}
	if err := s.requireFulfillableOrder(ctx, orderID); err != nil {
		return StatusResult{}, err
	}
	return s.applyLoaded(ctx, sh, previous, domainShipping.StatusDelivered, func(working *domainShipping.Shipment) error {
		return working.Deliver()
	})
}

// Cancel transitions pending → cancelled. Cancelled shipments cannot be reopened
// (order_id is unique; there is no recreate API). Order status is not checked —
// cancelling a shipment for an already-cancelled order is allowed.
func (s *StatusService) Cancel(ctx context.Context, orderID string) (StatusResult, error) {
	return s.apply(ctx, orderID, domainShipping.StatusCancelled, func(sh *domainShipping.Shipment) error {
		return sh.Cancel()
	})
}

func (s *StatusService) requireFulfillableOrder(ctx context.Context, orderID string) error {
	if s == nil || s.orders == nil {
		return nil
	}
	o, err := s.orders.FindByID(ctx, orderID)
	if err != nil {
		return err
	}
	if o == nil {
		return apperror.NotFound("order not found")
	}
	switch o.Status() {
	case domainOrder.OrderStatusCancelled, domainOrder.OrderStatusFailed:
		return apperror.Wrap(apperror.CodeValidation, ErrOrderNotFulfillable.Error(), ErrOrderNotFulfillable)
	default:
		return nil
	}
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
