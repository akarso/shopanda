package integrationdemo

import (
	"fmt"

	"github.com/akarso/shopanda/pkg/integrationhttp"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// Plugin demonstrates inbound ERP order status callbacks on integration routes.
type Plugin struct{}

// New returns the inbound integration reference plugin.
func New() pluginsdk.Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "integrationdemo/reference" }

func (p *Plugin) Init(h pluginsdk.Host) error {
	if h == nil {
		return fmt.Errorf("integrationdemo plugin: host not configured")
	}
	if !h.ConfigBool("plugins.integrationdemo.enabled") {
		return fmt.Errorf("integrationdemo plugin: disabled (plugins.integrationdemo.enabled=false)")
	}
	h.Require(pluginsdk.CapIntegrationOrder)
	h.Require(pluginsdk.CapIntegrationShipment)
	apiKey := h.ConfigValue("plugins.integrationdemo.integration_api_key")
	if apiKey == "" {
		return fmt.Errorf("integrationdemo plugin: integration_api_key is required")
	}
	hmacSecret := h.ConfigValue("plugins.integrationdemo.integration_hmac_secret")
	orderUpdater := h.IntegrationOrderStatusUpdater()
	if orderUpdater == nil {
		return fmt.Errorf("integrationdemo plugin: order status updater not configured")
	}
	shipmentUpdater := h.IntegrationShipmentStatusUpdater()
	if shipmentUpdater == nil {
		return fmt.Errorf("integrationdemo plugin: shipment status updater not configured")
	}

	replay := integrationhttp.NewMemoryReplayStore()
	auth := integrationhttp.AuthConfig{
		APIKey:      apiKey,
		HMACSecret:  hmacSecret,
		ReplayStore: replay,
		PluginSlug:  RouteSlug,
	}
	sdk := pluginsdk.NewHost(h, p.Name())
	orderHandler := NewOrderStatusHandler(orderUpdater, h.Logger())
	if err := sdk.Integration(RouteSlug).RegisterSecureRoute("POST", RouteOrderStatus, auth, orderHandler); err != nil {
		return fmt.Errorf("integrationdemo plugin: register order-status route: %w", err)
	}
	shipmentHandler := NewShipmentStatusHandler(shipmentUpdater, h.Logger())
	if err := sdk.Integration(RouteSlug).RegisterSecureRoute("POST", RouteShipmentStatus, auth, shipmentHandler); err != nil {
		return fmt.Errorf("integrationdemo plugin: register shipment-status route: %w", err)
	}
	if log := h.Logger(); log != nil {
		log.Info("integrationdemo plugin: inbound routes registered", map[string]interface{}{
			"order_status":    fmt.Sprintf("POST /api/v1/integrations/%s/%s", RouteSlug, RouteOrderStatus),
			"shipment_status": fmt.Sprintf("POST /api/v1/integrations/%s/%s", RouteSlug, RouteShipmentStatus),
		})
	}
	return nil
}
