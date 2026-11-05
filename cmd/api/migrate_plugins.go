package main

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// applyPluginMigrations collects plugin.Migrator specs from the compile-time
// registry (no InitAll — no brokers, licenses, or app wiring) and applies them
// after core migrate.Run.
func applyPluginMigrations(ctx context.Context, cfg *config.Config, log logger.Logger, conn *sql.DB) (plugin.PluginMigrateResult, error) {
	var zero plugin.PluginMigrateResult
	if cfg == nil {
		return zero, fmt.Errorf("migrate: config is required")
	}
	if conn == nil {
		return zero, fmt.Errorf("migrate: database is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	registry := plugin.NewRegistry(log)
	registerPlugins(registry, cfg)
	// Pass a minimal App only for depends_on ordering (Config).
	regs, err := registry.CollectMigrations(&plugin.App{Config: cfg})
	if err != nil {
		return zero, fmt.Errorf("migrate: %w", err)
	}
	totals, err := plugin.ApplyMigrationRegistrations(ctx, conn, log, regs)
	if err != nil {
		return totals, fmt.Errorf("migrate: %w", err)
	}
	return totals, nil
}
