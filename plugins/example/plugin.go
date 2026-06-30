package example

import (
	"fmt"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// PermissionReportsRead is an example plugin-defined admin permission.
const PermissionReportsRead rbac.Permission = "example.reports.read"

// Plugin demonstrates a third-party-style external plugin.
type Plugin struct{}

// New returns the example external plugin.
func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "example/demo" }

func (p *Plugin) Init(app *plugin.App) error {
	if app == nil {
		return fmt.Errorf("example plugin: app not configured")
	}
	if app.Config == nil {
		return fmt.Errorf("example plugin: config not configured")
	}
	if !app.Config.Plugins.Example.Enabled {
		return fmt.Errorf("example plugin: disabled (plugins.example.enabled=false)")
	}

	if err := app.RegisterConfig(examplePluginConfigDefinition()); err != nil {
		return fmt.Errorf("example plugin: register config: %w", err)
	}
	app.RegisterPricingStep(NewExampleFeeStep(&app.Config.Plugins.Example.FeeMinorUnits))
	if app.Bus != nil {
		app.Bus.OnAsync(order.EventOrderCreated, newOrderCreatedListener(app.Logger))
	}
	if err := app.RegisterPermission(PermissionReportsRead, identity.RoleAdmin); err != nil {
		return fmt.Errorf("example plugin: register permission: %w", err)
	}
	registerCLICommands(app)
	return nil
}
