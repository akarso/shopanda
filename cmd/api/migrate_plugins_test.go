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
	"github.com/akarso/shopanda/plugins/b2b"
)

func TestApplyPluginMigrations_SucceedsWithPromoAndIntegrationDemo(t *testing.T) {
	// Previously InitAll on the migrate path panicked/failed for these plugins
	// because the App lacked promotion/integration wiring. Migrator collection
	// must not call Init.
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
	if res.Applied == 0 && res.Adopted == 0 {
		// Fresh DB should apply; if already migrated by a prior test, second call is 0/0.
		res2, err := applyPluginMigrations(context.Background(), cfg, log, conn)
		if err != nil {
			t.Fatal(err)
		}
		if res2.Applied != 0 || res2.Adopted != 0 {
			t.Fatalf("idempotent apply = %+v, want 0/0", res2)
		}
	}

	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'customer_groups'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("B2B Migrator must create customer_groups without Init")
	}

	specs := b2b.New().Migrations()
	if len(specs) != 1 || specs[0].Dir != "migrations" {
		t.Fatalf("B2B Migrations() = %+v", specs)
	}
	if len(specs[0].Options.AdoptBareBasenames) != 4 {
		t.Fatalf("AdoptBareBasenames = %v, want 4 legacy files", specs[0].Options.AdoptBareBasenames)
	}
}
