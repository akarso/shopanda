package plugin

import (
	"github.com/akarso/shopanda/pkg/extapi"
)

// SetIntegrationShipmentStatusUpdater wires the shared shipment status updater before plugin Init.
func (a *App) SetIntegrationShipmentStatusUpdater(updater extapi.IntegrationShipmentStatusUpdater) {
	a.integrationShipmentStatusMu.Lock()
	defer a.integrationShipmentStatusMu.Unlock()
	a.integrationShipmentStatus = updater
}

// IntegrationShipmentStatusUpdater returns the shared shipment status updater.
func (a *App) IntegrationShipmentStatusUpdater() extapi.IntegrationShipmentStatusUpdater {
	a.integrationShipmentStatusMu.Lock()
	defer a.integrationShipmentStatusMu.Unlock()
	return a.integrationShipmentStatus
}
