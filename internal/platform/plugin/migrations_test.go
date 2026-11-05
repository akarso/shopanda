package plugin_test

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/migrate"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

func migrationTestLogger() logger.Logger {
	return logger.NewWithWriter(io.Discard, "error")
}

type migrationPlugin struct {
	name    string
	fsys    fstest.MapFS
	dir     string
	opts    migrate.PluginFSOptions
	initRan bool
}

func (p *migrationPlugin) Name() string { return p.name }

func (p *migrationPlugin) Migration() plugin.MigrationSpec {
	return plugin.MigrationSpec{FS: p.fsys, Dir: p.dir, Options: p.opts}
}

func (p *migrationPlugin) Init(*plugin.App) error {
	p.initRan = true
	return nil
}

type plainPlugin struct{ name string }

func (p *plainPlugin) Name() string           { return p.name }
func (p *plainPlugin) Init(*plugin.App) error { return nil }

type panickingMigrator struct{}

func (p *panickingMigrator) Name() string { return "shopanda/panic-mig" }
func (p *panickingMigrator) Init(*plugin.App) error {
	return nil
}
func (p *panickingMigrator) Migration() plugin.MigrationSpec {
	panic("migrations boom")
}

func TestCollectMigrations_SkipsNonMigrators(t *testing.T) {
	log := migrationTestLogger()
	reg := plugin.NewRegistry(log)
	reg.Register(&plainPlugin{name: "shopanda/plain"})
	reg.Register(&migrationPlugin{
		name: "shopanda/mig",
		fsys: fstest.MapFS{"migrations/001_a.sql": &fstest.MapFile{Data: []byte("SELECT 1;")}},
		dir:  "migrations",
	})
	regs, err := reg.CollectMigrations(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(regs) != 1 || regs[0].PluginName != "shopanda/mig" {
		t.Fatalf("CollectMigrations = %+v, want one shopanda/mig", regs)
	}
}

func TestCollectMigrations_DoesNotCallInit(t *testing.T) {
	p := &migrationPlugin{
		name: "shopanda/noinit",
		fsys: fstest.MapFS{"migrations/001_a.sql": &fstest.MapFile{Data: []byte("SELECT 1;")}},
		dir:  "migrations",
	}
	reg := plugin.NewRegistry(migrationTestLogger())
	reg.Register(p)
	if _, err := reg.CollectMigrations(nil); err != nil {
		t.Fatal(err)
	}
	if p.initRan {
		t.Fatal("CollectMigrations must not call Init")
	}
}

func TestCollectMigrations_RecoversPanic(t *testing.T) {
	reg := plugin.NewRegistry(migrationTestLogger())
	reg.Register(&panickingMigrator{})
	_, err := reg.CollectMigrations(nil)
	if err == nil {
		t.Fatal("expected panic to become error")
	}
	if !strings.Contains(err.Error(), "shopanda/panic-mig") {
		t.Fatalf("error = %v, want plugin name", err)
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Fatalf("error = %v, want panic", err)
	}
}

func TestCollectMigrations_RejectsNilFS(t *testing.T) {
	reg := plugin.NewRegistry(migrationTestLogger())
	reg.Register(&nilFSMigrator{})
	_, err := reg.CollectMigrations(nil)
	if err == nil {
		t.Fatal("expected error for nil FS")
	}
}

type nilFSMigrator struct{}

func (p *nilFSMigrator) Name() string           { return "shopanda/nilfs" }
func (p *nilFSMigrator) Init(*plugin.App) error { return nil }
func (p *nilFSMigrator) Migration() plugin.MigrationSpec {
	return plugin.MigrationSpec{Dir: "migrations"}
}

func TestCollectMigrations_DependsOnSortError(t *testing.T) {
	reg := plugin.NewRegistry(migrationTestLogger())
	reg.Register(&migrationPlugin{
		name: "shopanda/a",
		fsys: fstest.MapFS{"migrations/001_a.sql": &fstest.MapFile{Data: []byte("SELECT 1;")}},
		dir:  "migrations",
	})
	reg.Register(&migrationPlugin{
		name: "shopanda/b",
		fsys: fstest.MapFS{"migrations/001_b.sql": &fstest.MapFile{Data: []byte("SELECT 1;")}},
		dir:  "migrations",
	})
	_, err := reg.CollectMigrations(map[string][]string{
		"shopanda/a": {"shopanda/b"},
		"shopanda/b": {"shopanda/a"},
	})
	if err == nil {
		t.Fatal("expected depends_on cycle error")
	}
}

func TestInitAll_RecoversMigratorPanic(t *testing.T) {
	reg := plugin.NewRegistry(migrationTestLogger())
	reg.Register(&panickingMigrator{})
	app := &plugin.App{
		Logger:    migrationTestLogger(),
		Bootstrap: &plugin.Bootstrap{DB: &sql.DB{}}, // non-nil triggers apply; Migration() panics first
	}
	summary := reg.InitAll(app)
	if summary.MigrateFailed != 1 || summary.Failed != 1 {
		t.Fatalf("InitAll summary = %+v, want MigrateFailed=1", summary)
	}
	entries := reg.Entries()
	if len(entries) != 1 || entries[0].State != plugin.StateFailed {
		t.Fatalf("Entries() = %+v, want failed", entries)
	}
}

func TestInitAll_SkipPluginMigrations(t *testing.T) {
	p := &migrationPlugin{
		name: "shopanda/skip",
		fsys: fstest.MapFS{
			"migrations/001_x.sql": &fstest.MapFile{Data: []byte("SELECT 1/0;")},
		},
		dir: "migrations",
	}
	reg := plugin.NewRegistry(migrationTestLogger())
	reg.Register(p)
	app := &plugin.App{
		Logger:               migrationTestLogger(),
		SkipPluginMigrations: true,
		Bootstrap:            &plugin.Bootstrap{DB: &sql.DB{}},
	}
	summary := reg.InitAll(app)
	if summary.Failed != 0 || !p.initRan {
		t.Fatalf("summary=%+v initRan=%v", summary, p.initRan)
	}
}

func TestInitAll_AppliesMigratorBeforeInit(t *testing.T) {
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
	pluginName := "shopanda/boot-" + suffix
	basename := "001_boot.sql"
	version := "plugin:" + pluginName + ":" + basename
	table := "pr1086_boot_" + suffix
	t.Cleanup(func() {
		_, _ = dbConn.Exec("DROP TABLE IF EXISTS " + table)
		_, _ = dbConn.Exec("DELETE FROM schema_migrations WHERE version = $1", version)
	})

	p := &migrationPlugin{
		name: pluginName,
		fsys: fstest.MapFS{
			"migrations/" + basename: &fstest.MapFile{Data: []byte("CREATE TABLE " + table + " (id int);")},
		},
		dir: "migrations",
	}
	log := migrationTestLogger()
	reg := plugin.NewRegistry(log)
	reg.Register(p)
	app := &plugin.App{
		Logger:    log,
		Config:    &config.Config{},
		Bootstrap: &plugin.Bootstrap{DB: dbConn},
	}
	if summary := reg.InitAll(app); summary.Failed != 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll = %+v", summary)
	}
	if !p.initRan {
		t.Fatal("Init was not called")
	}
	var n int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1`, table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("Migrator SQL must run before Init completes")
	}

	totals, err := plugin.ApplyMigrationRegistrations(context.Background(), dbConn, log, []plugin.MigrationRegistration{{
		PluginName: pluginName,
		FS:         p.fsys,
		Dir:        p.dir,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if totals.Applied != 0 || totals.Adopted != 0 {
		t.Fatalf("second apply totals = %+v, want 0/0", totals)
	}
}

func TestApplyMigrationRegistrations_StopsOnError(t *testing.T) {
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
	okName := "shopanda/ok-" + suffix
	badName := "shopanda/bad-" + suffix
	okTable := "pr1086_ok_" + suffix
	okVersion := "plugin:" + okName + ":001_ok.sql"
	t.Cleanup(func() {
		_, _ = dbConn.Exec("DROP TABLE IF EXISTS " + okTable)
		_, _ = dbConn.Exec("DELETE FROM schema_migrations WHERE version = $1", okVersion)
	})

	regs := []plugin.MigrationRegistration{
		{
			PluginName: okName,
			FS: fstest.MapFS{
				"migrations/001_ok.sql": &fstest.MapFile{Data: []byte("CREATE TABLE " + okTable + " (id int);")},
			},
			Dir: "migrations",
		},
		{
			PluginName: badName,
			FS: fstest.MapFS{
				"migrations/001_bad.sql": &fstest.MapFile{Data: []byte("SELECT 1/0;")},
			},
			Dir: "migrations",
		},
	}
	_, err = plugin.ApplyMigrationRegistrations(context.Background(), dbConn, migrationTestLogger(), regs)
	if err == nil {
		t.Fatal("expected error from second registration")
	}
	if !strings.Contains(err.Error(), badName) {
		t.Fatalf("error = %v, want plugin name %s", err, badName)
	}
}
