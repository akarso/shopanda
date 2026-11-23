package extapi

import "time"

// OrderView is a minimal order read model for plugin integrations.
//
// ContactEmail is intentionally omitted: pluginsdk.OrderPort (the default
// composition-root adapter) returns this struct with no email set, since any
// plugin that declares Need(CapOrders) gets this port and PII should not
// ride along by default. There is no privileged accessor yet for the real
// address — add one (gated by a narrower capability) if a concrete
// integration needs it.
type OrderView struct {
	ID         string
	CustomerID string
	Status     string
	Currency   string
	CreatedAt  time.Time
}

// OrderLineView is one order line for OrderPort.Lines.
// Line identity is (order_id, variant_id) — order items have no separate id.
// Extensions maps field code → value and includes public (non-private) fields
// plus PluginOnlyWrite fields owned by the calling plugin; other plugins'
// plugin-only fields are omitted (same rule as CartView).
type OrderLineView struct {
	VariantID  string
	SKU        string
	Name       string
	Quantity   int
	Extensions map[string]interface{}
}
