package b2b_test

import (
	"database/sql"
	"io"
	"os"
	"testing"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/migrate"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/b2b"

	_ "github.com/lib/pq"
)

func testApp(cfg *config.Config, db *sql.DB) *plugin.App {
	log := logger.NewWithWriter(io.Discard, "error")
	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: cfg,
	}
	if db != nil {
		app.Bootstrap = &plugin.Bootstrap{DB: db}
	}
	return app
}

func TestPlugin_Name(t *testing.T) {
	if got := b2b.New().Name(); got != "shopanda/b2b" {
		t.Fatalf("Name() = %q, want shopanda/b2b", got)
	}
}

func TestPlugin_Init_DisabledReturnsError(t *testing.T) {
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			B2B: config.B2BPluginConfig{Enabled: false},
		},
	}
	if err := b2b.New().Init(testApp(cfg, nil)); err == nil {
		t.Fatal("Init() expected error when plugins.b2b.enabled is false")
	}
}

func TestPlugin_Init_InvalidLicenseReturnsError(t *testing.T) {
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			B2B: config.B2BPluginConfig{Enabled: true, LicenseKey: ""},
		},
	}
	if err := b2b.New().Init(testApp(cfg, nil)); err == nil {
		t.Fatal("Init() expected error for missing license key")
	}
}

func TestPlugin_Init_MissingDBReturnsError(t *testing.T) {
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			B2B: config.B2BPluginConfig{Enabled: true, LicenseKey: "DEV-local"},
		},
	}
	if err := b2b.New().Init(testApp(cfg, nil)); err == nil {
		t.Fatal("Init() expected error when database bootstrap is missing")
	}
}

func TestPlugin_Init_RegistersPermissionsAndRoutes(t *testing.T) {
	rbac.ResetPluginPermissions()
	t.Cleanup(rbac.ResetPluginPermissions)

	dsn := os.Getenv("SHOPANDA_TEST_DSN")
	if dsn == "" {
		t.Skip("SHOPANDA_TEST_DSN not set; skipping integration test")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("ping db: %v", err)
	}
	if _, err := migrate.Run(db, "../../migrations"); err != nil {
		t.Fatalf("run core migrations: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM customer_group_members`)
		_, _ = db.Exec(`DELETE FROM customer_groups`)
	})

	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			B2B: config.B2BPluginConfig{Enabled: true, LicenseKey: "DEV-local"},
		},
	}
	app := testApp(cfg, db)
	if err := b2b.New().Init(app); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	if !rbac.HasPermission(identity.RoleAdmin, b2b.PermissionGroupsRead) {
		t.Fatal("expected b2b.groups.read for admin")
	}
	if !rbac.HasPermission(identity.RoleManager, b2b.PermissionGroupsWrite) {
		t.Fatal("expected b2b.groups.write for manager")
	}

	routes := app.AdminRoutes()
	if len(routes) != 7 {
		t.Fatalf("AdminRoutes() len = %d, want 7", len(routes))
	}
}
