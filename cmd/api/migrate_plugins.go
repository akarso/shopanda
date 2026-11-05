package main

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// applyPluginMigrations initializes compile-time plugins so they can
// RegisterMigrations, then applies every registered plugin FS (idempotent with
// the Init safety-net apply). Call after core migrate.Run.
func applyPluginMigrations(cfg *config.Config, log logger.Logger, conn *sql.DB) error {
	if cfg == nil {
		return fmt.Errorf("migrate: config is required")
	}
	if conn == nil {
		return fmt.Errorf("migrate: database is required")
	}
	registry := plugin.NewRegistry(log)
	registerPlugins(registry, cfg)
	boot, err := newPluginBootstrap(conn)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	pluginApp := &plugin.App{
		Logger:    log,
		Config:    cfg,
		Bootstrap: boot,
	}
	preparePermissionRegistry(pluginApp)
	summary := registry.InitAll(pluginApp)
	freezePermissionRegistry(pluginApp)
	if summary.Failed > 0 {
		return fmt.Errorf("migrate: plugin init failed: %d plugin(s) failed to initialize", summary.Failed)
	}
	if err := pluginApp.ApplyMigrations(context.Background(), conn); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}
