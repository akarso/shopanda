package extapi

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
}
