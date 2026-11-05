package shipping

import "errors"

var (
	// ErrOrderIDRequired is returned when an order id is missing.
	ErrOrderIDRequired = errors.New("order id must not be empty")
	// ErrTrackingRequired is returned when MarkShipped is called without a tracking number.
	ErrTrackingRequired = errors.New("tracking number must not be empty")
	// ErrTrackingAmendNotSupported is returned when an already-shipped shipment is
	// re-submitted with a different tracking number or a conflicting non-empty provider_ref.
	ErrTrackingAmendNotSupported = errors.New("tracking amend not supported")
	// ErrOrderNotFulfillable is returned when ship/deliver is attempted on a
	// cancelled or failed order.
	ErrOrderNotFulfillable = errors.New("order cannot be fulfilled")
)
