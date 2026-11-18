package main

import (
	"database/sql"
	"io"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/platform/cli"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// Acceptance (PR-1575): CapCatalogSeed Need boots via the shared serve/worker
// skip-list helpers; seed CLI assert without CatalogSeedPort fails closed
// through the same assert bindPluginCLIPortsFromBoot uses after bindSeed.

func TestBootServePlugins_CapCatalogSeedOK(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	reg.Register(pluginsdk.Adapt(needsCartSDKPlugin{}))
	reg.Register(pluginsdk.Adapt(needsCatalogSeedSDKPlugin{}))
	app := &plugin.App{Logger: log}

	if err := bootServePlugins(reg, app, nil, struct{}{}); err != nil {
		t.Fatalf("bootServePlugins: %v", err)
	}
	if _, ok := app.CatalogSeedPort(); ok {
		t.Fatal("serve boot must leave CatalogSeedPort unbound")
	}
}

func TestBootServePlugins_NilCartPortFailsWhenCapCartNeeded(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	reg.Register(pluginsdk.Adapt(needsCartSDKPlugin{}))
	app := &plugin.App{Logger: log}

	if err := bootServePlugins(reg, app, nil, nil); err == nil {
		t.Fatal("bootServePlugins: expected error for nil cartPort")
	}
}

func TestBootWorkerPlugins_CapCatalogSeedOK(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	reg.Register(pluginsdk.Adapt(needsCatalogSeedSDKPlugin{}))
	reg.Register(pluginsdk.Adapt(needsCartSDKPlugin{}))
	app := &plugin.App{Logger: log}

	if err := bootWorkerPlugins(reg, app, nil); err != nil {
		t.Fatalf("bootWorkerPlugins: %v", err)
	}
	if _, ok := app.CatalogSeedPort(); ok {
		t.Fatal("worker boot must leave CatalogSeedPort unbound")
	}
}

func TestRunPluginCLICommand_SeedNeedWithoutBindFailsAssert(t *testing.T) {
	probe := &seedCLIProbePlugin{}
	reg, app := initSeedCLIRuntime(t, probe)

	deps := pluginCLIDeps{
		OpenDB: stubPluginCLIDeps(t).OpenDB,
		Bind: func(a *plugin.App, _ *sql.DB, cmd cli.Command) error {
			boot := stubSeedBoot()
			a.Bootstrap = boot
			if err := bindProductPorts(a, boot); err != nil {
				return err
			}
			// Omit bindCatalogSeedPort — same Needs assert
			// bindPluginCLIPortsFromBoot runs after bindSeed=true.
			return assertPluginCLINeedsMet(a, true)
		},
	}

	ran, err := runPluginCLICommandWithDeps(&config.Config{}, logger.NewWithWriter(io.Discard, "error"),
		func() (*pluginCLIRuntime, error) {
			return &pluginCLIRuntime{Registry: reg, App: app}, nil
		}, "test:seed-probe", nil, deps)
	if !ran || err == nil || !strings.Contains(err.Error(), "plugin capabilities") {
		t.Fatalf("ran=%v err=%v, want plugin capabilities fail-closed without CatalogSeedPort", ran, err)
	}
	if _, ok := app.CatalogSeedPort(); ok {
		t.Fatal("CatalogSeedPort must stay unbound when Bind skips seed")
	}
}
