package pluginsdk

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
)

// AsHost wraps *plugin.App as a Host without attributing calls to a specific
// plugin. Test-only / unscoped: no production code path reaches it — real
// plugin loading always goes through AsHostFor via Adapt/sdk.New, and
// pkg/pluginsdk/harness uses AsHostFor as well. AsHost exists only for
// direct-call unit tests that construct a Host without going through Adapt.
// Because no plugin name is attached, ConfigValue sees the full config (same
// as before PR-1546 review) and the service-port getters
// (Catalog/Orders/Customers/BootstrapDB) are not gated by a declared Need —
// prefer AsHostFor for anything that should be scoped to one plugin's config
// namespace and declared capabilities.
func AsHost(app *plugin.App) Host {
	if app == nil {
		panic("pluginsdk: AsHost: app must not be nil")
	}
	return &appHost{app: app}
}

// AsHostFor wraps *plugin.App as a Host scoped to pluginName: ConfigValue
// only sees that plugin's own `plugins.<slug>.*` config namespace (see
// configSlug), and Catalog/Orders/Customers/Cart/BootstrapDB getters return
// ok=false unless pluginName declared the matching capability. Used by
// Adapt so every real plugin Init goes through a scoped Host. AsHost stays
// available for tests that construct a Host directly (unscoped).
func AsHostFor(app *plugin.App, pluginName string) Host {
	if app == nil {
		panic("pluginsdk: AsHostFor: app must not be nil")
	}
	return &appHost{app: app, pluginName: pluginName}
}

type appHost struct {
	app        *plugin.App
	pluginName string
	flatOnce   sync.Once
	flat       map[string]string // filled once via flatOnce for ConfigValue
}

// configSlug resolves plugins.<slug>.* for pluginName.
//
// Conventions in this repo:
//   - "sitekit" → sitekit
//   - "extref/reference", "cartdemo/reference", "example/demo" → first segment
//     (extref, cartdemo, example) — the trailing "reference"/"demo" label is
//     not a config namespace
//   - "shopanda/b2b" → b2b (vendor/product: always the last segment)
//
// Vendor/product names always use the last segment so a future
// plugins.shopanda.* key cannot hijack shopanda/b2b into reading
// plugins.shopanda.* and failing Init as disabled.
func (h *appHost) configSlug() string {
	if h.pluginName == "" {
		return ""
	}
	name := h.pluginName
	if !strings.Contains(name, "/") {
		return name
	}
	first := name
	last := name
	if i := strings.IndexByte(name, '/'); i >= 0 {
		first = name[:i]
		last = name[i+1:]
		if j := strings.LastIndexByte(last, '/'); j >= 0 {
			last = last[j+1:]
		}
	}
	if last == "reference" || last == "demo" {
		return first
	}
	return last
}

func (h *appHost) flatConfig() map[string]string {
	if h.app == nil || h.app.Config == nil {
		return nil
	}
	h.flatOnce.Do(func() {
		h.flat = config.Flatten(h.app.Config)
	})
	return h.flat
}

func (h *appHost) Context() context.Context { return h.app.Context() }
func (h *appHost) Logger() Logger           { return h.app.Logger }

// RegisterPricingStep panics when step cannot be coerced to a pipeline step
// (same fail-closed style as plugin.App's nil-step / bad-position panics):
// silently registering an uncoercible step would otherwise only surface much
// later as a dropped step at wiring time ("plugin.step.invalid_type", logged
// and skipped — see cmd/api providers.go), which a plugin author would never
// see during Init.
func (h *appHost) RegisterPricingStep(step any, position ...string) {
	coerced, err := coercePricingStep(step)
	if err != nil {
		panic("pluginsdk: RegisterPricingStep: " + err.Error())
	}
	h.app.RegisterPricingStep(coerced, position...)
}

// RegisterCheckoutStep panics when step cannot be coerced; see RegisterPricingStep.
func (h *appHost) RegisterCheckoutStep(step any, position ...string) {
	coerced, err := coerceCheckoutStep(step)
	if err != nil {
		panic("pluginsdk: RegisterCheckoutStep: " + err.Error())
	}
	h.app.RegisterCheckoutStep(coerced, position...)
}

// RegisterCompositionStep panics when step cannot be coerced; see RegisterPricingStep.
func (h *appHost) RegisterCompositionStep(pipeline string, step any) {
	coerced, err := coerceCompositionStep(pipeline, step)
	if err != nil {
		panic("pluginsdk: RegisterCompositionStep: " + err.Error())
	}
	h.app.RegisterCompositionStep(pipeline, coerced)
}

func (h *appHost) RegisterTaxCalculator(calc extapi.TaxCalculator) {
	h.app.RegisterTaxCalculator(AdaptTaxCalculator(calc))
}

func (h *appHost) RegisterMailSender(sender extapi.MailSender) {
	h.app.RegisterMailSender(AdaptMailSender(sender))
}

func (h *appHost) RegisterConfig(def extapi.ConfigDefinition) error {
	internalDef, err := adaptConfigDefinition(def)
	if err != nil {
		return err
	}
	return h.app.RegisterConfig(internalDef)
}

func (h *appHost) Slots(registrant string) SlotsAPI {
	return h.app.Slots(registrant)
}

func (h *appHost) RegisterPublicRoute(pattern string, handler http.Handler) error {
	return h.app.RegisterPublicRoute(pattern, handler)
}

func (h *appHost) RegisterAdminRoute(pattern string, permission string, handler http.Handler) error {
	return h.app.RegisterAdminRoute(pattern, rbac.Permission(permission), handler)
}

func (h *appHost) RegisterPermission(permission string, roles ...string) error {
	rs := make([]identity.Role, 0, len(roles))
	for _, r := range roles {
		rs = append(rs, identity.Role(r))
	}
	return h.app.RegisterPermission(rbac.Permission(permission), rs...)
}

func (h *appHost) Hooks(registrant string) HookAPI {
	return h.app.Hooks(registrant)
}

func (h *appHost) RegisterField(def extapi.FieldDef) error {
	return h.app.Extensions().RegisterField(fieldDefToDomain(def))
}

func (h *appHost) RegisterFieldFor(owner string, def extapi.FieldDef) error {
	return h.app.ExtensionsFor(owner).RegisterField(fieldDefToDomain(def))
}

func (h *appHost) PluginWriteContext(ctx context.Context, pluginName string, fieldCodes ...string) (context.Context, error) {
	return h.app.PluginWriteContext(ctx, pluginName, fieldCodes...)
}

func (h *appHost) WithPluginWrite(ctx context.Context, pluginName string, fieldCodes ...string) context.Context {
	return h.app.WithPluginWrite(ctx, pluginName, fieldCodes...)
}

// Require records that this Host's plugin depends on cap. Attributed
// directly to h.pluginName (not App.CurrentPluginName()) so attribution and
// the gated getters below work the same whether this runs inside a real
// InitAllContext boot or a direct-call unit test that never goes through the
// registry loop.
func (h *appHost) Require(cap Capability) {
	h.app.RecordNeedsFor(h.pluginName, cap)
}

func (h *appHost) Cart() (CartPort, bool) {
	if !h.declaredOrUnscoped(plugin.CapCart) {
		return nil, false
	}
	p, ok := h.app.CartPort()
	if !ok {
		return nil, false
	}
	cp, ok := p.(CartPort)
	return cp, ok
}

// declaredOrUnscoped reports whether it's safe to hand this plugin a
// composition-root port: either the Host isn't attributed to a specific
// plugin (AsHost — tests/harness), or pluginName declared cap via
// NeedsDeclarer/Host.Require. A plugin cannot read a port just because some
// other plugin's Need happened to cause the root to bind it.
func (h *appHost) declaredOrUnscoped(cap plugin.Capability) bool {
	if h.pluginName == "" {
		return true
	}
	return h.app.CapabilityDeclaredBy(cap, h.pluginName)
}

func (h *appHost) Catalog() (CatalogPort, bool) {
	if !h.declaredOrUnscoped(plugin.CapCatalog) {
		return nil, false
	}
	p, ok := h.app.CatalogPort()
	if !ok {
		return nil, false
	}
	cp, ok := p.(CatalogPort)
	return cp, ok
}

func (h *appHost) Orders() (OrderPort, bool) {
	if !h.declaredOrUnscoped(plugin.CapOrders) {
		return nil, false
	}
	p, ok := h.app.OrdersPort()
	if !ok {
		return nil, false
	}
	cp, ok := p.(OrderPort)
	return cp, ok
}

func (h *appHost) Customers() (CustomerPort, bool) {
	if !h.declaredOrUnscoped(plugin.CapCustomers) {
		return nil, false
	}
	p, ok := h.app.CustomerPort()
	if !ok {
		return nil, false
	}
	cp, ok := p.(CustomerPort)
	return cp, ok
}

func (h *appHost) IntegrationOrderStatusUpdater() extapi.IntegrationOrderStatusUpdater {
	if !h.declaredOrUnscoped(plugin.CapIntegrationOrder) {
		return nil
	}
	return h.app.IntegrationOrderStatusUpdater()
}

func (h *appHost) IntegrationShipmentStatusUpdater() extapi.IntegrationShipmentStatusUpdater {
	if !h.declaredOrUnscoped(plugin.CapIntegrationShipment) {
		return nil
	}
	return h.app.IntegrationShipmentStatusUpdater()
}

func (h *appHost) IntegrationStockSyncer() extapi.IntegrationStockSyncer {
	if !h.declaredOrUnscoped(plugin.CapIntegrationStock) {
		return nil
	}
	return h.app.IntegrationStockSyncer()
}

// ConfigValue reads a config value, scoped to this plugin's own
// `plugins.<slug>.*` namespace when the Host was created via AsHostFor
// (every plugin loaded through Adapt). A bare AsHost Host (no plugin name
// attached — tests/harness) sees the full unredacted config, matching the
// pre-PR-1546-review behavior; real plugin Init calls cannot read another
// plugin's or core's config (database password, other plugins' API keys,
// webhook/tracing secrets, etc.) through this method anymore.
func (h *appHost) ConfigValue(key string) string {
	if h.app == nil || h.app.Config == nil {
		return ""
	}
	if slug := h.configSlug(); slug != "" {
		prefix := "plugins." + slug + "."
		if key != "plugins."+slug && !strings.HasPrefix(key, prefix) {
			return ""
		}
	}
	flat := h.flatConfig()
	if flat == nil {
		return ""
	}
	return flat[key]
}

func (h *appHost) ConfigBool(key string) bool {
	b, _ := strconv.ParseBool(h.ConfigValue(key))
	return b
}

func (h *appHost) ConfigInt(key string) int {
	n, _ := strconv.Atoi(h.ConfigValue(key))
	return n
}

func (h *appHost) ConfigInt64(key string) int64 {
	n, _ := strconv.ParseInt(h.ConfigValue(key), 10, 64)
	return n
}

func (h *appHost) BootstrapDB() (*sql.DB, bool) {
	if !h.declaredOrUnscoped(plugin.CapDB) {
		return nil, false
	}
	if h.app == nil || h.app.Bootstrap == nil || h.app.Bootstrap.DB == nil {
		return nil, false
	}
	return h.app.Bootstrap.DB, true
}

// coreApp returns the underlying *plugin.App for SDK helpers not yet on Host
// (import/export/sync/promotion). Callers outside this package must not use it.
func coreApp(h Host) *plugin.App {
	ah, ok := h.(*appHost)
	if !ok || ah.app == nil {
		panic("pluginsdk: host must be created with AsHost")
	}
	return ah.app
}
