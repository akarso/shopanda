package pluginsdk

import "github.com/akarso/shopanda/internal/platform/plugin"

// Capability names a composition-root facility plugins may depend on.
type Capability = plugin.Capability

const (
	CapCart                = plugin.CapCart
	CapCatalog             = plugin.CapCatalog
	CapCatalogSeed         = plugin.CapCatalogSeed
	CapOrders              = plugin.CapOrders
	CapCustomers           = plugin.CapCustomers
	CapDB                  = plugin.CapDB
	CapJobs                = plugin.CapJobs
	CapMedia               = plugin.CapMedia
	CapIntegrationOrder    = plugin.CapIntegrationOrder
	CapIntegrationShipment = plugin.CapIntegrationShipment
	CapIntegrationStock    = plugin.CapIntegrationStock
)

// NeedsDeclarer is optional on pluginsdk.Plugin implementations.
// Declared needs are recorded before Init; the composition root must bind
// matching ports (see plugin.App.AssertNeedsMet).
type NeedsDeclarer interface {
	Needs() []Capability
}
