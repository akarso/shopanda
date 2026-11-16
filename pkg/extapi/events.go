package extapi

// Stable platform event names plugins may subscribe sync jobs to via
// SDK.Integration(slug).RegisterOnEvent. Mirrors of internal/domain event
// constants — the string values are part of the stable contract.
const (
	// EventOrderCreated fires when a new order is created.
	EventOrderCreated = "order.created"
)
