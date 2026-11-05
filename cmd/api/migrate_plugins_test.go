package main

import (
	"context"
	"database/sql"
	"io"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/migrate"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/b2b"
)

func TestCollectMigrations_PromoIntegrationAndB2B_NoInit(t *testing.T) {
	// No DSN: collecting Migrator specs must not call Init (promodemo /
	// integrationdemo would panic or fail without full App wiring).
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			PromoDemo:       config.PromoDemoPluginConfig{Enabled: true},
			IntegrationDemo: config.IntegrationDemoPluginConfig{Enabled: true},
			B2B:             config.B2BPluginConfig{Enabled: true, LicenseKey: "unused"},
		},
	}
	log := logger.NewWithWriter(io.Discard, "error")
	registry := plugin.NewRegistry(log)
	registerPlugins(registry, cfg)
	regs, err := registry.CollectMigrations(cfg.Plugins.DependsOn)
	if err != nil {
		t.Fatal(err)
	}
	if len(regs) != 1 || regs[0].PluginName != "shopanda/b2b" {
		t.Fatalf("CollectMigrations = %+v, want only shopanda/b2b", regs)
	}
	spec := b2b.New().Migration()
	if regs[0].Dir != spec.Dir || len(regs[0].Options.AdoptBareBasenames) != len(spec.Options.AdoptBareBasenames) {
		t.Fatalf("registration drifted from B2B Migration(): %+v vs %+v", regs[0], spec)
	}
}

func TestApplyPluginMigrations_SucceedsWithPromoAndIntegrationDemo(t *testing.T) {
	dsn := os.Getenv("SHOPANDA_TEST_DSN")
	if dsn == "" {
		t.Skip("SHOPANDA_TEST_DSN not set")
	}
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.Ping(); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			PromoDemo:       config.PromoDemoPluginConfig{Enabled: true},
			IntegrationDemo: config.IntegrationDemoPluginConfig{Enabled: true},
			B2B:             config.B2BPluginConfig{Enabled: false},
		},
	}
	log := logger.NewWithWriter(io.Discard, "error")
	res, err := applyPluginMigrations(context.Background(), cfg, log, conn)
	if err != nil {
		t.Fatalf("applyPluginMigrations: %v", err)
	}
	if res.Applied != 0 || res.Adopted != 0 {
		t.Fatalf("totals = %+v, want 0/0 with no Migrator plugins", res)
	}
}

func TestApplyPluginMigrations_AppliesB2BWithoutInitWiring(t *testing.T) {
	dsn := os.Getenv("SHOPANDA_TEST_DSN")
	if dsn == "" {
		t.Skip("SHOPANDA_TEST_DSN not set")
	}
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.Ping(); err != nil {
		t.Fatal(err)
	}
	// go test cwd is this package directory.
	if _, err := migrate.Run(conn, "../../migrations"); err != nil {
		t.Fatalf("core migrations: %v", err)
	}

	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			PromoDemo:       config.PromoDemoPluginConfig{Enabled: true},
			IntegrationDemo: config.IntegrationDemoPluginConfig{Enabled: true},
			B2B:             config.B2BPluginConfig{Enabled: true, LicenseKey: "unused-on-migrate-path"},
		},
	}
	log := logger.NewWithWriter(io.Discard, "error")
	res, err := applyPluginMigrations(context.Background(), cfg, log, conn)
	if err != nil {
		t.Fatalf("applyPluginMigrations: %v", err)
	}

	res2, err := applyPluginMigrations(context.Background(), cfg, log, conn)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Applied != 0 || res2.Adopted != 0 {
		t.Fatalf("second apply = %+v, want 0/0 (first was %+v)", res2, res)
	}

	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'customer_groups'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("B2B Migrator must create customer_groups without Init")
	}
}
