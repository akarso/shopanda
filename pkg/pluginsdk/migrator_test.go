package pluginsdk_test

import (
	"io"
	"testing"
	"testing/fstest"

	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
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
