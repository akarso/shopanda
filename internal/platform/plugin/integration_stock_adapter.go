package plugin

import (
	inventoryApp "github.com/akarso/shopanda/internal/application/inventory"
	"github.com/akarso/shopanda/pkg/extapi"
)

// NewIntegrationStockSyncer adapts the core stock sync service for integration plugins.
func NewIntegrationStockSyncer(svc *inventoryApp.StockSyncService) extapi.IntegrationStockSyncer {
	if svc == nil {
		panic("plugin: integration stock sync service must not be nil")
	}
	return svc
}
