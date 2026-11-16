package warehousedemo

import (
	"fmt"

	sdkhttp "github.com/akarso/shopanda/pkg/integrationsdk/http"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// Plugin demonstrates outbound warehouse stock sync via RegisterSyncJob.
type Plugin struct{}

// New returns the warehouse stock reference plugin.
func New() pluginsdk.Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "warehousedemo/reference" }

func (p *Plugin) Init(h pluginsdk.Host) error {
	if h == nil {
		return fmt.Errorf("warehousedemo plugin: host not configured")
	}
	if !h.ConfigBool("plugins.warehousedemo.enabled") {
		return fmt.Errorf("warehousedemo plugin: disabled (plugins.warehousedemo.enabled=false)")
	}
	h.Require(pluginsdk.CapIntegrationStock)
	baseURL := h.ConfigValue("plugins.warehousedemo.warehouse_base_url")
	if baseURL == "" {
		return fmt.Errorf("warehousedemo plugin: warehouse_base_url is required")
	}
	syncer := h.IntegrationStockSyncer()
	if syncer == nil {
		return fmt.Errorf("warehousedemo plugin: stock syncer not configured")
	}

	apiKey := h.ConfigValue("plugins.warehousedemo.warehouse_api_key")
	headers := map[string]string{}
	if apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	}
	client, err := sdkhttp.New(sdkhttp.Config{
		BaseURL: baseURL,
		Headers: headers,
		Logger:  h.Logger(),
	})
	if err != nil {
		return fmt.Errorf("warehousedemo plugin: http client: %w", err)
	}

	cronSpec := h.ConfigValue("plugins.warehousedemo.sync_cron")
	if cronSpec == "" {
		cronSpec = "@every 5m"
	}
	handler := NewStockSyncHandler(client, syncer, h.Logger())
	if err := pluginsdk.NewHost(h, p.Name()).Integration(RouteSlug).RegisterCron(SyncJobStock, cronSpec, handler); err != nil {
		return fmt.Errorf("warehousedemo plugin: register stock sync job: %w", err)
	}
	if log := h.Logger(); log != nil {
		log.Info("warehousedemo plugin: stock sync job registered", map[string]interface{}{
			"job_type":  fmt.Sprintf("integration.sync.%s.%s", RouteSlug, SyncJobStock),
			"cron_spec": cronSpec,
		})
	}
	return nil
}
