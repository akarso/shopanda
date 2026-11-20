package pluginsdk_test

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// sdkMigratorOnApp implements plugin.Plugin + pluginsdk.Migrator only (no
// pluginsdk.Adapt). MigrationSpec alias must make CollectMigrations see it.
type sdkMigratorOnApp struct {
	fsys fstest.MapFS
}

func (sdkMigratorOnApp) Name() string { return "test/sdk-only-mig" }

func (sdkMigratorOnApp) Init(*plugin.App) error { return nil }

func (p sdkMigratorOnApp) Migration() pluginsdk.MigrationSpec {
	return pluginsdk.MigrationSpec{FS: p.fsys, Dir: "migrations"}
}

func TestMigrationSpec_AliasesPluginMigrationSpec(t *testing.T) {
	var _ plugin.Migrator = sdkMigratorOnApp{}
}

func TestCollectMigrations_PluginAppSDKMigrator(t *testing.T) {
	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	reg.Register(sdkMigratorOnApp{
		fsys: fstest.MapFS{
			"migrations/001_noop.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
		},
	})
	regs, err := reg.CollectMigrations(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(regs) != 1 || regs[0].PluginName != "test/sdk-only-mig" {
		t.Fatalf("CollectMigrations = %+v, want test/sdk-only-mig", regs)
	}
	if regs[0].FS == nil || regs[0].Dir != "migrations" {
		t.Fatalf("registration incomplete: %+v", regs[0])
	}
}

// InitAllContext apply path must see pluginsdk.Migrator on a raw plugin.App
// (same type-assert as CollectMigrations via the MigrationSpec alias).
func TestInitAllContext_PluginAppSDKMigratorApplies(t *testing.T) {
	dsn := os.Getenv("SHOPANDA_TEST_DSN")
	if dsn == "" {
		t.Skip("SHOPANDA_TEST_DSN not set")
	}
	dbConn, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbConn.Close() })
	if err := dbConn.Ping(); err != nil {
		t.Fatal(err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	pluginName := "test/sdk-boot-" + suffix
	basename := "001_sdk_boot.sql"
	version := "plugin:" + pluginName + ":" + basename
	table := "pr1589_sdk_" + suffix
	t.Cleanup(func() {
		_, _ = dbConn.Exec("DROP TABLE IF EXISTS " + table)
		_, _ = dbConn.Exec("DELETE FROM schema_migrations WHERE version = $1", version)
	})

	p := namedSDKMigrator{
		name: pluginName,
		fsys: fstest.MapFS{
			"migrations/" + basename: &fstest.MapFile{Data: []byte("CREATE TABLE " + table + " (id int);")},
		},
	}
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	reg.Register(p)
	app := &plugin.App{
		Logger:    log,
		Config:    &config.Config{},
		Bootstrap: &plugin.Bootstrap{DB: dbConn},
	}
	summary := reg.InitAllContext(context.Background(), app)
	if err := plugin.InitSummaryError(summary); err != nil {
		t.Fatalf("InitAllContext: %v summary=%+v", err, summary)
	}
	if summary.Initialized != 1 {
		t.Fatalf("InitAllContext = %+v, want Initialized=1", summary)
	}
	var n int
	if err := dbConn.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_name = $1", table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("table %s missing after InitAllContext apply", table)
	}
}

type namedSDKMigrator struct {
	name string
	fsys fstest.MapFS
}

func (p namedSDKMigrator) Name() string { return p.name }

func (namedSDKMigrator) Init(*plugin.App) error { return nil }

func (p namedSDKMigrator) Migration() pluginsdk.MigrationSpec {
	return pluginsdk.MigrationSpec{FS: p.fsys, Dir: "migrations"}
}
