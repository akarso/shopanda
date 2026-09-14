package main

import (
	"database/sql"
	"fmt"

	inventoryApp "github.com/akarso/shopanda/internal/application/inventory"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// wireIntegrationStockSyncer registers a StockSyncService on app for
// plugin ERP sync integrations, and returns the concrete service so a
// caller that constructs a *searchApp.ReindexService later in its own
// setup sequence (wireServeRuntime does — see its own call site) can
// still wire it in via SetReindexService (PR-1049), without having to
// reorder construction so ReindexService exists before this is called.
func wireIntegrationStockSyncer(app *plugin.App, variants catalog.VariantRepository, stock inventory.StockRepository) *inventoryApp.StockSyncService {
	svc := inventoryApp.NewStockSyncService(variants, stock)
	app.SetIntegrationStockSyncer(plugin.NewIntegrationStockSyncer(svc))
	return svc
}

func wireIntegrationStockSyncerFromDB(conn *sql.DB, app *plugin.App) (*inventoryApp.StockSyncService, error) {
	variantRepo, err := postgres.NewVariantRepo(conn)
	if err != nil {
		return nil, fmt.Errorf("integration stock syncer variant repo: %w", err)
	}
	stockRepo, err := postgres.NewStockRepo(conn)
	if err != nil {
		return nil, fmt.Errorf("integration stock syncer stock repo: %w", err)
	}
	return wireIntegrationStockSyncer(app, variantRepo, stockRepo), nil
}
