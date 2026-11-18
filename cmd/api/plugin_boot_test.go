package main

import (
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// Acceptance (PR-1575): CapCatalogSeed Need boots via the shared serve/worker
// helpers; seed-CLI assert without CatalogSeedPort fails closed.

func TestBootServePlugins_CapCatalogSeedOK(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	reg.Register(pluginsdk.Adapt(needsCatalogSeedSDKPlugin{}))
	app := &plugin.App{Logger: log}

	if err := bootServePlugins(reg, app, nil, struct{}{}); err != nil {
		t.Fatalf("bootServePlugins: %v", err)
	}
	if _, ok := app.CatalogSeedPort(); ok {
		t.Fatal("serve boot must leave CatalogSeedPort unbound")
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

func TestAssertSeedCLINeedsMet_UnboundCatalogSeedFails(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	reg.Register(pluginsdk.Adapt(needsCatalogSeedSDKPlugin{}))
	app := &plugin.App{Logger: log}
	if err := bindProductPorts(app, nil); err != nil {
		t.Fatalf("bindProductPorts: %v", err)
	}
	summary := reg.InitAll(app)
	if summary.Failed != 0 {
		t.Fatalf("InitAll summary = %+v", summary)
	}
	if err := assertSeedCLINeedsMet(app); err == nil {
		t.Fatal("assertSeedCLINeedsMet: expected CapCatalogSeed unbound error")
	}
}
