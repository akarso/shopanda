package extapi

// CustomerView is a read-only customer snapshot for plugin ports.
//
// Email is intentionally omitted: pluginsdk.CustomerPort (the default
// composition-root adapter) returns this struct with no email set, since any
// plugin that declares Need(CapCustomers) gets this port and PII should not
// ride along by default. There is no privileged accessor yet for the real
// address — add one (gated by a narrower capability) if a concrete
// integration needs it.
type CustomerView struct {
	ID string
}
