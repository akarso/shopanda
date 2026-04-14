package plugin

import (
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
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
	Logger logger.Logger
	Bus    *event.Bus
	Config *config.Config

	pricingSteps     []any
	checkoutSteps    []any
	compositionSteps map[string][]any
}

// RegisterPricingStep registers a pricing pipeline step.
// The step must implement pricing.PricingStep.
func (a *App) RegisterPricingStep(step any) {
	if step == nil {
		panic("plugin: pricing step must not be nil")
	}
	a.pricingSteps = append(a.pricingSteps, step)
}

// RegisterCheckoutStep registers a checkout workflow step.
// The step must implement checkout.Step.
func (a *App) RegisterCheckoutStep(step any) {
	if step == nil {
		panic("plugin: checkout step must not be nil")
	}
	a.checkoutSteps = append(a.checkoutSteps, step)
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

// PricingSteps returns a copy of the registered pricing steps.
func (a *App) PricingSteps() []any {
	return append([]any(nil), a.pricingSteps...)
}

// CheckoutSteps returns a copy of the registered checkout steps.
func (a *App) CheckoutSteps() []any {
	return append([]any(nil), a.checkoutSteps...)
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
