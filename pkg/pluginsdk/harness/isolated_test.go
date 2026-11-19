package harness_test

import (
	"os"
	"testing"
	"testing/fstest"

	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/pkg/pluginsdk/harness"
)

func TestOpenIsolated_SkipWithoutDSN(t *testing.T) {
	prev, had := os.LookupEnv("SHOPANDA_TEST_DSN")
	_ = os.Unsetenv("SHOPANDA_TEST_DSN")
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("SHOPANDA_TEST_DSN", prev)
		}
	})

	ok := t.Run("unset", func(t *testing.T) {
		harness.OpenIsolated(t, harness.IsolatedOptions{CoreMigrationsDir: "-"})
		t.Fatal("OpenIsolated should have skipped when SHOPANDA_TEST_DSN unset")
	})
	if !ok {
		t.Fatal("nested OpenIsolated failed instead of skipping")
	}
}

func TestOpenIsolated_SchemaPinned(t *testing.T) {
	if os.Getenv("SHOPANDA_TEST_DSN") == "" {
		t.Skip("SHOPANDA_TEST_DSN not set")
	}
	iso := harness.OpenIsolated(t, harness.IsolatedOptions{
		CoreMigrationsDir: "-",
		PluginName:        "harnessdemo",
		PluginMigration: pluginsdk.MigrationSpec{
			FS: fstest.MapFS{
				"migrations/001_noop.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
			},
			Dir: "migrations",
		},
	})
	var current string
	if err := iso.DB.QueryRow("SELECT current_schema()").Scan(&current); err != nil {
		t.Fatalf("current_schema: %v", err)
	}
	if current != iso.Schema {
		t.Fatalf("current_schema = %q, want %q", current, iso.Schema)
	}
}

func TestAdaptInit_WithDB_BootstrapDB(t *testing.T) {
	if os.Getenv("SHOPANDA_TEST_DSN") == "" {
		t.Skip("SHOPANDA_TEST_DSN not set")
	}
	iso := harness.OpenIsolated(t, harness.IsolatedOptions{CoreMigrationsDir: "-"})
	p := &dbNeedPlugin{}
	res, err := harness.AdaptInit(p, harness.Options{
		DB:      iso.DB,
		Satisfy: []pluginsdk.Capability{pluginsdk.CapDB},
	})
	if err != nil {
		t.Fatalf("AdaptInit: %v", err)
	}
	db, ok := res.Host.BootstrapDB()
	if !ok || db != iso.DB {
		t.Fatalf("BootstrapDB = (%v, %v)", db, ok)
	}
}

type dbNeedPlugin struct{}

func (dbNeedPlugin) Name() string { return "harness/dbneed" }
func (p *dbNeedPlugin) Init(h pluginsdk.Host) error {
	h.Require(pluginsdk.CapDB)
	return nil
}
