package plugin

import (
	"github.com/akarso/shopanda/pkg/extapi"
)

// SetIntegrationOrderStatusUpdater wires the shared order status updater before plugin Init.
func (a *App) SetIntegrationOrderStatusUpdater(updater extapi.IntegrationOrderStatusUpdater) {
	a.integrationOrderStatusMu.Lock()
	defer a.integrationOrderStatusMu.Unlock()
	a.integrationOrderStatus = updater
}

// IntegrationOrderStatusUpdater returns the shared order status updater.
func (a *App) IntegrationOrderStatusUpdater() extapi.IntegrationOrderStatusUpdater {
	a.integrationOrderStatusMu.Lock()
	defer a.integrationOrderStatusMu.Unlock()
	return a.integrationOrderStatus
}
