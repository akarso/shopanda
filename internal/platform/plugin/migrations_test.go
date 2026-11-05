package plugin_test

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
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
	fsys    fs.FS
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

type typedNilFSMigrator struct{}

func (p *typedNilFSMigrator) Name() string { return "shopanda/typednil" }
func (p *typedNilFSMigrator) Init(*plugin.App) error {
	return nil
}
func (p *typedNilFSMigrator) Migration() plugin.MigrationSpec {
	var m *fstest.MapFS
	return plugin.MigrationSpec{FS: m, Dir: "migrations"}
}

func TestCollectMigrations_SkipsNonMigrators(t *testing.T) {
	reg := plugin.NewRegistry(migrationTestLogger())
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
	if !strings.Contains(err.Error(), "shopanda/panic-mig") || !strings.Contains(err.Error(), "panic") {
		t.Fatalf("error = %v", err)
	}
}

func TestCollectMigrations_RejectsTypedNilFS(t *testing.T) {
	reg := plugin.NewRegistry(migrationTestLogger())
	reg.Register(&typedNilFSMigrator{})
	_, err := reg.CollectMigrations(nil)
	if err == nil {
		t.Fatal("expected typed-nil FS error")
	}
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

func TestInitAll_DependsOnSortErrorWithMigrator(t *testing.T) {
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
	app := &plugin.App{
		Logger: migrationTestLogger(),
		Config: &config.Config{
			Plugins: config.PluginsConfig{
				DependsOn: map[string][]string{
					"shopanda/a": {"shopanda/b"},
					"shopanda/b": {"shopanda/a"},
				},
			},
		},
		Bootstrap: &plugin.Bootstrap{DB: &sql.DB{}},
	}
	summary := reg.InitAll(app)
	if summary.DependsOnErr == nil {
		t.Fatal("expected DependsOnErr")
	}
	if err := plugin.InitSummaryError(summary); err == nil {
		t.Fatal("InitSummaryError should surface DependsOnErr")
	}
	if summary.Initialized != 0 {
		t.Fatalf("Initialized = %d, want 0", summary.Initialized)
	}
	if summary.Failed != summary.Registered {
		t.Fatalf("Failed = %d, want Registered=%d so Failed-only callers refuse", summary.Failed, summary.Registered)
	}
}

// No Bootstrap.DB: Migrator SQL is skipped, but a depends_on error must still
// refuse Init so import/export/CLI callers cannot silently drop hooks.
func TestInitAll_DependsOnSortErrorSkipsInitWithoutDB(t *testing.T) {
	ok := &migrationPlugin{
		name: "shopanda/b2b-like",
		fsys: fstest.MapFS{"migrations/001_a.sql": &fstest.MapFile{Data: []byte("SELECT 1;")}},
		dir:  "migrations",
	}
	hookRan := false
	hooksOK := &initRecorder{name: "shopanda/hooks", ran: &hookRan}
	reg := plugin.NewRegistry(migrationTestLogger())
	reg.Register(hooksOK)
	reg.Register(ok)
	app := &plugin.App{
		Logger: migrationTestLogger(),
		Config: &config.Config{
			Plugins: config.PluginsConfig{
				DependsOn: map[string][]string{
					"shopanda/hooks": {"shopanda/disabled-plugin"},
				},
			},
		},
	}
	summary := reg.InitAll(app)
	if summary.DependsOnErr == nil || summary.Failed == 0 || summary.Initialized != 0 {
		t.Fatalf("summary = %+v, want DependsOnErr + Failed>0 + Initialized=0", summary)
	}
	if hookRan {
		t.Fatal("Init must not run when depends_on sort fails with a Migrator registered")
	}
}

type initRecorder struct {
	name string
	ran  *bool
}

func (p *initRecorder) Name() string { return p.name }
func (p *initRecorder) Init(*plugin.App) error {
	*p.ran = true
	return nil
}

func TestInitAll_EntriesStateUnderDependsOn(t *testing.T) {
	ok := &migrationPlugin{
		name: "shopanda/ok-dep",
		fsys: fstest.MapFS{"migrations/001_a.sql": &fstest.MapFile{Data: []byte("SELECT 1;")}},
		dir:  "migrations",
	}
	bad := &failInitPlugin{name: "shopanda/bad-dep"}
	reg := plugin.NewRegistry(migrationTestLogger())
	reg.Register(ok)
	reg.Register(bad)
	app := &plugin.App{
		Logger: migrationTestLogger(),
		Config: &config.Config{
			Plugins: config.PluginsConfig{
				DependsOn: map[string][]string{
					"shopanda/bad-dep": {"shopanda/ok-dep"},
				},
			},
		},
		PluginMigrateMode: plugin.PluginMigrateVerify, // no DB apply; Init still runs
	}
	summary := reg.InitAll(app)
	if summary.Initialized != 1 || summary.Failed != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	byName := map[string]plugin.Entry{}
	for _, e := range reg.Entries() {
		byName[e.Name] = e
	}
	if byName["shopanda/ok-dep"].State != plugin.StateActive {
		t.Fatalf("ok state = %s", byName["shopanda/ok-dep"].State)
	}
	if byName["shopanda/bad-dep"].State != plugin.StateFailed {
		t.Fatalf("bad state = %s", byName["shopanda/bad-dep"].State)
	}
}

type failInitPlugin struct{ name string }

func (p *failInitPlugin) Name() string { return p.name }
func (p *failInitPlugin) Init(*plugin.App) error {
	return fmt.Errorf("init boom")
}

func TestInitAll_RecoversMigratorPanic(t *testing.T) {
	reg := plugin.NewRegistry(migrationTestLogger())
	reg.Register(&panickingMigrator{})
	app := &plugin.App{
		Logger:    migrationTestLogger(),
		Bootstrap: &plugin.Bootstrap{DB: &sql.DB{}},
	}
	summary := reg.InitAll(app)
	if summary.MigrateFailed != 1 || summary.Failed != 1 {
		t.Fatalf("InitAll summary = %+v, want MigrateFailed=1", summary)
	}
	if err := plugin.InitSummaryError(summary); err == nil {
		t.Fatal("InitSummaryError should fail")
	}
}

func TestInitAll_VerifyModePending(t *testing.T) {
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
	if _, err := dbConn.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	name := "shopanda/verify-" + suffix
	p := &migrationPlugin{
		name: name,
		fsys: fstest.MapFS{"migrations/001_x.sql": &fstest.MapFile{Data: []byte("SELECT 1;")}},
		dir:  "migrations",
	}
	reg := plugin.NewRegistry(migrationTestLogger())
	reg.Register(p)
	app := &plugin.App{
		Logger:            migrationTestLogger(),
		Bootstrap:         &plugin.Bootstrap{DB: dbConn},
		PluginMigrateMode: plugin.PluginMigrateVerify,
	}
	summary := reg.InitAll(app)
	if summary.MigrateFailed != 1 {
		t.Fatalf("summary = %+v, want MigrateFailed=1 for pending", summary)
	}
	if summary.Failed == 0 || !strings.Contains(reg.Entries()[0].Err.Error(), "run app migrate") {
		t.Fatalf("want run app migrate error, got %+v", reg.Entries()[0].Err)
	}
}

func TestInitAllContext_Cancelled(t *testing.T) {
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
	name := "shopanda/cancel-" + suffix
	p := &migrationPlugin{
		name: name,
		fsys: fstest.MapFS{
			"migrations/001_sleep.sql": &fstest.MapFile{Data: []byte("SELECT pg_sleep(30);")},
		},
		dir: "migrations",
	}
	reg := plugin.NewRegistry(migrationTestLogger())
	reg.Register(p)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app := &plugin.App{
		Logger:    migrationTestLogger(),
		Bootstrap: &plugin.Bootstrap{DB: dbConn},
	}
	summary := reg.InitAllContext(ctx, app)
	if summary.MigrateFailed != 1 {
		t.Fatalf("summary = %+v, want MigrateFailed on cancelled ctx", summary)
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

func TestApplyMigrationRegistrations_StopsOnErrorKeepsTotals(t *testing.T) {
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
	totals, err := plugin.ApplyMigrationRegistrations(context.Background(), dbConn, migrationTestLogger(), regs)
	if err == nil {
		t.Fatal("expected error from second registration")
	}
	if !strings.Contains(err.Error(), badName) {
		t.Fatalf("error = %v, want plugin name %s", err, badName)
	}
	if totals.Applied != 1 {
		t.Fatalf("totals.Applied = %d, want 1 from first registration", totals.Applied)
	}
}

func TestInitSummaryError_MigrateFailed(t *testing.T) {
	err := plugin.InitSummaryError(plugin.InitSummary{MigrateFailed: 2, Failed: 2})
	if err == nil || !strings.Contains(err.Error(), "migrate failed") {
		t.Fatalf("got %v", err)
	}
}
