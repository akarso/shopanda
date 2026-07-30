package plugin

import (
	"sync"

	"github.com/akarso/shopanda/internal/application/extension"
	assetsapp "github.com/akarso/shopanda/internal/application/assets"
	hooksapp "github.com/akarso/shopanda/internal/application/hooks"
	importctxapp "github.com/akarso/shopanda/internal/application/importctx"
	exportctxapp "github.com/akarso/shopanda/internal/application/exportctx"
	slotsapp "github.com/akarso/shopanda/internal/application/slots"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/promotion"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/platform/cli"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/integrationhttp"
)

// Plugin defines the contract for extending the system.
// Plugins register behavior during Init and are then invoked by the system.
type Plugin interface {
	// Name returns a unique identifier for the plugin.
	Name() string

	// Init initializes the plugin. Called once at startup.
	// The plugin should use the provided App to register event handlers,
	// pipeline steps, or other extensions.
	// Returning an error disables the plugin without crashing the system.
	Init(app *App) error
}

// App provides the system facilities available to plugins during initialization.
type App struct {
	Logger    logger.Logger
	Bus       *event.Bus
	Config    *config.Config
	Bootstrap *Bootstrap

	pricingSteps     []pricingStepRegistration
	checkoutSteps    []checkoutStepRegistration
	compositionSteps map[string][]any

	searchProvider  any
	cache           any
	queue           any
	paymentRegistry *payment.ProviderRegistry
	shippingRegistry *shipping.ProviderRegistry
	mediaStorage    any
	taxCalculator   any
	mailSender      any

	configRegistry *ConfigRegistry

	adminRoutes  []AdminRoute
	publicRoutes []PublicRoute
	cliRegistry  *cli.Registry

	extensionRegistry   *extension.Registry
	extensionRegistryMu sync.Mutex

	hookRegistry   *hooksapp.Registry
	hookRegistryMu sync.Mutex

	slotRegistry   *slotsapp.Registry
	slotRegistryMu sync.Mutex

	assetRegistry   *assetsapp.Registry
	assetRegistryMu sync.Mutex

	importRegistry   *importctxapp.Registry
	importRegistryMu sync.Mutex

	exportRegistry   *exportctxapp.Registry
	exportRegistryMu sync.Mutex

	promotionEvaluators   *promotion.EvaluatorRegistry
	promotionEvaluatorsMu sync.Mutex

	integrationIdempotency   integrationhttp.IdempotencyStore
	integrationIdempotencyMu sync.Mutex

	integrationOrderStatus   extapi.IntegrationOrderStatusUpdater
	integrationOrderStatusMu sync.Mutex

	integrationStockSync   extapi.IntegrationStockSyncer
	integrationStockSyncMu sync.Mutex

	syncJobs []SyncJobRegistration
}

// RegisterCompositionStep registers a composition pipeline step.
// Pipeline names: "pdp" (product detail), "plp" (product listing).
// The step must implement the appropriate composition.Step[T].
func (a *App) RegisterCompositionStep(pipeline string, step any) {
	if pipeline == "" {
		panic("plugin: composition pipeline name must not be empty")
	}
	if step == nil {
		panic("plugin: composition step must not be nil")
	}
	if a.compositionSteps == nil {
		a.compositionSteps = make(map[string][]any)
	}
	a.compositionSteps[pipeline] = append(a.compositionSteps[pipeline], step)
}

// CompositionSteps returns a copy of the registered composition steps for a pipeline.
func (a *App) CompositionSteps(pipeline string) []any {
	if a.compositionSteps == nil {
		return nil
	}
	s := a.compositionSteps[pipeline]
	if s == nil {
		return nil
	}
	return append([]any(nil), s...)
}

// RegisterPermission registers a plugin-defined permission and the roles
// that are granted it. The permission must not conflict with core permissions.
func (a *App) RegisterPermission(perm rbac.Permission, roles ...identity.Role) error {
	return rbac.RegisterPluginPermission(perm, roles...)
}

// RegisterConfig registers admin-editable settings for this plugin.
func (a *App) RegisterConfig(def ConfigDefinition) error {
	if a.configRegistry == nil {
		a.configRegistry = NewConfigRegistry()
	}
	return a.configRegistry.Register(def)
}
