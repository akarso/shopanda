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

func wireIntegrationStockSyncer(app *plugin.App, variants catalog.VariantRepository, stock inventory.StockRepository) {
	svc := inventoryApp.NewStockSyncService(variants, stock)
	app.SetIntegrationStockSyncer(plugin.NewIntegrationStockSyncer(svc))
}

func wireIntegrationStockSyncerFromDB(conn *sql.DB, app *plugin.App) error {
	variantRepo, err := postgres.NewVariantRepo(conn)
	if err != nil {
		return fmt.Errorf("integration stock syncer variant repo: %w", err)
	}
	stockRepo, err := postgres.NewStockRepo(conn)
	if err != nil {
		return fmt.Errorf("integration stock syncer stock repo: %w", err)
	}
	wireIntegrationStockSyncer(app, variantRepo, stockRepo)
	return nil
}
