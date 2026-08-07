package warehousedemo

import (
	"fmt"

	"github.com/akarso/shopanda/internal/platform/plugin"
	sdkhttp "github.com/akarso/shopanda/pkg/integrationsdk/http"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// Plugin demonstrates outbound warehouse stock sync via RegisterSyncJob.
type Plugin struct{}

// New returns the warehouse stock reference plugin.
func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "warehousedemo/reference" }

func (p *Plugin) Init(app *plugin.App) error {
	if app == nil {
		return fmt.Errorf("warehousedemo plugin: app not configured")
	}
	if app.Config == nil {
		return fmt.Errorf("warehousedemo plugin: config not configured")
	}
	cfg := app.Config.Plugins.WarehouseDemo
	if !cfg.Enabled {
		return fmt.Errorf("warehousedemo plugin: disabled (plugins.warehousedemo.enabled=false)")
	}
	if cfg.WarehouseBaseURL == "" {
		return fmt.Errorf("warehousedemo plugin: warehouse_base_url is required")
	}
	syncer := app.IntegrationStockSyncer()
	if syncer == nil {
		return fmt.Errorf("warehousedemo plugin: stock syncer not configured")
	}

	headers := map[string]string{}
	if cfg.WarehouseAPIKey != "" {
		headers["Authorization"] = "Bearer " + cfg.WarehouseAPIKey
	}
	client, err := sdkhttp.New(sdkhttp.Config{
		BaseURL: cfg.WarehouseBaseURL,
		Headers: headers,
		Logger:  app.Logger,
	})
	if err != nil {
		return fmt.Errorf("warehousedemo plugin: http client: %w", err)
	}

	cronSpec := cfg.SyncCron
	if cronSpec == "" {
		cronSpec = "@every 5m"
	}
	handler := NewStockSyncHandler(client, syncer, app.Logger)
	if err := pluginsdk.New(app, p.Name()).Integration(RouteSlug).RegisterCron(SyncJobStock, cronSpec, handler); err != nil {
		return fmt.Errorf("warehousedemo plugin: register stock sync job: %w", err)
	}
	if app.Logger != nil {
		app.Logger.Info("warehousedemo plugin: stock sync job registered", map[string]interface{}{
			"job_type":  fmt.Sprintf("integration.sync.%s.%s", RouteSlug, SyncJobStock),
			"cron_spec": cronSpec,
		})
	}
	return nil
}
