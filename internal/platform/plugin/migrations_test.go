package plugin_test

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"testing"
	"testing/fstest"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/migrate"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

func migrationTestLogger() logger.Logger {
	return logger.NewWithWriter(io.Discard, "error")
}

type migrationPlugin struct {
	name string
	fsys fstest.MapFS
	dir  string
	opts migrate.PluginFSOptions
}

func (p *migrationPlugin) Name() string { return p.name }

func (p *migrationPlugin) Init(app *plugin.App) error {
	return app.RegisterMigrations(p.name, p.fsys, p.dir, p.opts)
}

func TestRegisterMigrations_RequiresPluginName(t *testing.T) {
	app := &plugin.App{}
	err := app.RegisterMigrations("", fstest.MapFS{
		"migrations/001_a.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
	}, "migrations", migrate.PluginFSOptions{})
	if err == nil {
		t.Fatal("expected error for empty plugin name")
	}
}

func TestRegisterMigrations_RejectsNameMismatchDuringInitAll(t *testing.T) {
	log := migrationTestLogger()
	reg := plugin.NewRegistry(log)
	reg.Register(&mismatchMigrationPlugin{})
	app := &plugin.App{Logger: log}
	summary := reg.InitAll(app)
	if summary.Failed != 1 {
		t.Fatalf("InitAll summary = %+v, want Failed=1", summary)
	}
}

type mismatchMigrationPlugin struct{}

func (p *mismatchMigrationPlugin) Name() string { return "shopanda/real" }

func (p *mismatchMigrationPlugin) Init(app *plugin.App) error {
	return app.RegisterMigrations("shopanda/other", fstest.MapFS{
		"migrations/001_a.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
	}, "migrations", migrate.PluginFSOptions{})
}

func TestRegisterMigrations_ViaInitAll(t *testing.T) {
	log := migrationTestLogger()
	reg := plugin.NewRegistry(log)
	p := &migrationPlugin{
		name: "shopanda/migtest",
		fsys: fstest.MapFS{
			"migrations/001_a.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
		},
		dir: "migrations",
	}
	reg.Register(p)
	app := &plugin.App{Logger: log}
	summary := reg.InitAll(app)
	if summary.Failed != 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll summary = %+v", summary)
	}
	got := app.Migrations()
	if len(got) != 1 {
		t.Fatalf("Migrations() len = %d, want 1", len(got))
	}
	if got[0].PluginName != p.name || got[0].Dir != "migrations" {
		t.Fatalf("registration = %+v", got[0])
	}
}

func TestRegisterMigrations_DuplicateRejected(t *testing.T) {
	log := migrationTestLogger()
	reg := plugin.NewRegistry(log)
	reg.Register(&dupMigrationPlugin{})
	app := &plugin.App{Logger: log}
	summary := reg.InitAll(app)
	if summary.Failed != 1 {
		t.Fatalf("InitAll summary = %+v, want Failed=1", summary)
	}
}

type dupMigrationPlugin struct{}

func (p *dupMigrationPlugin) Name() string { return "shopanda/dupmig" }

func (p *dupMigrationPlugin) Init(app *plugin.App) error {
	fsys := fstest.MapFS{"migrations/001_a.sql": &fstest.MapFile{Data: []byte("SELECT 1;")}}
	if err := app.RegisterMigrations(p.Name(), fsys, "migrations", migrate.PluginFSOptions{}); err != nil {
		return err
	}
	return app.RegisterMigrations(p.Name(), fsys, "migrations", migrate.PluginFSOptions{})
}

func TestApplyMigrations_AppliesRegisteredFS(t *testing.T) {
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
	pluginName := "shopanda/apply-" + suffix
	basename := "001_apply.sql"
	version := "plugin:" + pluginName + ":" + basename
	table := "pr1086_apply_" + suffix

	t.Cleanup(func() {
		_, _ = dbConn.Exec("DROP TABLE IF EXISTS " + table)
		_, _ = dbConn.Exec("DELETE FROM schema_migrations WHERE version = $1", version)
	})

	log := migrationTestLogger()
	reg := plugin.NewRegistry(log)
	reg.Register(&migrationPlugin{
		name: pluginName,
		fsys: fstest.MapFS{
			"migrations/" + basename: &fstest.MapFile{Data: []byte("CREATE TABLE " + table + " (id int);")},
		},
		dir: "migrations",
	})
	app := &plugin.App{Logger: log}
	if summary := reg.InitAll(app); summary.Failed != 0 {
		t.Fatalf("InitAll: %+v", summary)
	}

	if err := app.ApplyMigrations(context.Background(), dbConn); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}
	var n int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = $1`, version).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("version count = %d, want 1", n)
	}
	if err := app.ApplyMigrations(context.Background(), dbConn); err != nil {
		t.Fatalf("second ApplyMigrations: %v", err)
	}
}
