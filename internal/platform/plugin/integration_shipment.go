package plugin

import (
	"github.com/akarso/shopanda/pkg/extapi"
)

// SetIntegrationShipmentStatusUpdater wires the shared shipment status updater before plugin Init.
func (a *App) SetIntegrationShipmentStatusUpdater(updater extapi.IntegrationShipmentStatusUpdater) {
	a.integrationShipmentStatusMu.Lock()
	a.integrationShipmentStatus = updater
	a.integrationShipmentStatusMu.Unlock()
	if updater != nil {
		a.MarkCapabilitySatisfied(CapIntegrationShipment)
	}
}

// IntegrationShipmentStatusUpdater returns the shared shipment status updater.
func (a *App) IntegrationShipmentStatusUpdater() extapi.IntegrationShipmentStatusUpdater {
	a.integrationShipmentStatusMu.Lock()
	defer a.integrationShipmentStatusMu.Unlock()
	return a.integrationShipmentStatus
}
