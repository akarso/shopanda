package pluginsdk

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/akarso/shopanda/pkg/extapi"
)

// Logger is the structured logging surface available to plugins via Host.
type Logger interface {
	Info(event string, ctx map[string]interface{})
	Warn(event string, ctx map[string]interface{})
	Error(event string, err error, ctx map[string]interface{})
}

// HookAPI registers hook handlers without importing internal/application/hooks.
type HookAPI interface {
	Register(hook extapi.HookPoint, priority int, handler extapi.HookHandler) error
}

// Host is the public Init surface for product plugins (Track E).
// Implementations live in core; plugins depend only on pkg/pluginsdk + pkg/extapi.
type Host interface {
	Context() context.Context
	Logger() Logger

	RegisterPricingStep(step any, position ...string)
	RegisterCheckoutStep(step any, position ...string)
	RegisterCompositionStep(pipeline string, step any)

	// RegisterTaxCalculator replaces the core tax calculation port.
	RegisterTaxCalculator(calc extapi.TaxCalculator)
	// RegisterMailSender replaces the core mail delivery port.
	RegisterMailSender(sender extapi.MailSender)

	RegisterPublicRoute(pattern string, handler http.Handler) error
	RegisterAdminRoute(pattern string, permission string, handler http.Handler) error
	RegisterPermission(permission string, roles ...string) error

	// RegisterConfig registers admin-editable settings for this plugin.
	RegisterConfig(def extapi.ConfigDefinition) error

	// Slots returns storefront slot renderer registration scoped to registrant.
	Slots(registrant string) SlotsAPI

	Hooks(registrant string) HookAPI

	// RegisterField registers an extension field owned by the current Init plugin
	// when called during Init (loader assigns owner); use ExtensionsFor semantics
	// via RegisterFieldFor when registering outside Init.
	RegisterField(def extapi.FieldDef) error
	RegisterFieldFor(owner string, def extapi.FieldDef) error

	// PluginWriteContext marks ctx as a trusted write for pluginName + field codes.
	// Prefer this in recovered paths (async handlers). Trust is stripped at bus
	// Publish, pricing Pipeline.Execute, checkout Workflow.Execute, jobs Worker,
	// and for cart hooks/pricing (retained only for extension upsert).
	PluginWriteContext(ctx context.Context, pluginName string, fieldCodes ...string) (context.Context, error)
	// WithPluginWrite is PluginWriteContext that panics on error (Init / sync On).
	WithPluginWrite(ctx context.Context, pluginName string, fieldCodes ...string) context.Context

	// Require records that this plugin depends on a composition-root capability.
	// The root must bind matching ports before AssertNeedsMet succeeds.
	Require(cap Capability)

	Cart() (CartPort, bool)
	Catalog() (CatalogPort, bool)
	Orders() (OrderPort, bool)
	Customers() (CustomerPort, bool)

	// IntegrationOrderStatusUpdater, IntegrationShipmentStatusUpdater, and
	// IntegrationStockSyncer return the composition-root-bound integration
	// write ports for inbound/outbound integration plugins (nil when unbound).
	IntegrationOrderStatusUpdater() extapi.IntegrationOrderStatusUpdater
	IntegrationShipmentStatusUpdater() extapi.IntegrationShipmentStatusUpdater
	IntegrationStockSyncer() extapi.IntegrationStockSyncer

	// ConfigValue reads a dot-notation config value (e.g. "plugins.acme.enabled")
	// from this app's loaded config, or "" when unset. ConfigBool/Int/Int64
	// parse the same value, returning the zero value on a missing/invalid entry.
	ConfigValue(key string) string
	ConfigBool(key string) bool
	ConfigInt(key string) int
	ConfigInt64(key string) int64

	// BootstrapDB returns the composition-root database handle, if configured
	// (e.g. for a Migrator plugin's integration-test helpers).
	BootstrapDB() (*sql.DB, bool)

	// OnQueueBound registers a wrap applied after queue resolve and before
	// BindQueue (claim fence). wrap receives and must return the queue
	// instance (jobs.Queue). Nil wrap is ignored.
	OnQueueBound(wrap func(queue any) any)
	// OnRequeued registers a callback run under the reclaim lock before
	// status flips to pending (attempt is the claim being released).
	// Non-nil error or panic aborts Requeue. Not called for Admin.Retry.
	OnRequeued(fn func(ctx context.Context, jobID string, attempt int) error)
}

// SlotsAPI registers storefront slot renderers without importing
// internal/application/slots.
type SlotsAPI interface {
	RegisterRenderer(anchor extapi.SlotAnchor, placement extapi.Placement, priority int, renderer extapi.SlotRenderer) error
}

// Plugin is the public plugin contract: Init receives Host, not *internal plugin.App.
type Plugin interface {
	Name() string
	Init(Host) error
}
