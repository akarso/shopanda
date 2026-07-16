package plugin

import (
	"github.com/akarso/shopanda/pkg/extapi"
)

// SetIntegrationStockSyncer wires the shared stock syncer before plugin Init.
func (a *App) SetIntegrationStockSyncer(syncer extapi.IntegrationStockSyncer) {
	a.integrationStockSyncMu.Lock()
	defer a.integrationStockSyncMu.Unlock()
	a.integrationStockSync = syncer
}

// IntegrationStockSyncer returns the shared stock syncer.
func (a *App) IntegrationStockSyncer() extapi.IntegrationStockSyncer {
	a.integrationStockSyncMu.Lock()
	defer a.integrationStockSyncMu.Unlock()
	return a.integrationStockSync
}
