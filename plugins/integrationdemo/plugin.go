package integrationdemo

import (
	"fmt"

	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/integrationhttp"
)

// Plugin demonstrates inbound ERP order status callbacks on integration routes.
type Plugin struct{}

// New returns the inbound integration reference plugin.
func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "integrationdemo/reference" }

func (p *Plugin) Init(app *plugin.App) error {
	if app == nil {
		return fmt.Errorf("integrationdemo plugin: app not configured")
	}
	if app.Config == nil {
		return fmt.Errorf("integrationdemo plugin: config not configured")
	}
	cfg := app.Config.Plugins.IntegrationDemo
	if !cfg.Enabled {
		return fmt.Errorf("integrationdemo plugin: disabled (plugins.integrationdemo.enabled=false)")
	}
	if cfg.IntegrationAPIKey == "" {
		return fmt.Errorf("integrationdemo plugin: integration_api_key is required")
	}
	updater := app.IntegrationOrderStatusUpdater()
	if updater == nil {
		return fmt.Errorf("integrationdemo plugin: order status updater not configured")
	}

	replay := integrationhttp.NewMemoryReplayStore()
	auth := integrationhttp.AuthConfig{
		APIKey:      cfg.IntegrationAPIKey,
		HMACSecret:  cfg.IntegrationHMACSecret,
		ReplayStore: replay,
		PluginSlug:  RouteSlug,
	}
	handler := NewOrderStatusHandler(updater, app.Logger)
	if err := app.Integration(RouteSlug).RegisterSecureRoute("POST", RouteOrderStatus, auth, handler); err != nil {
		return fmt.Errorf("integrationdemo plugin: register order-status route: %w", err)
	}
	if app.Logger != nil {
		app.Logger.Info("integrationdemo plugin: inbound order-status route registered", map[string]interface{}{
			"route": fmt.Sprintf("POST /api/v1/integrations/%s/%s", RouteSlug, RouteOrderStatus),
		})
	}
	return nil
}
