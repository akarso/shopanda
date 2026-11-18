package main

import (
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// needsCartSDKPlugin mirrors plugins/extref: it declares Needs(CapCart) and
// calls Host.Require(CapCart) during Init, without otherwise depending on a
// bound CartPort.
type needsCartSDKPlugin struct{}

func (needsCartSDKPlugin) Name() string { return "test/needscart" }

func (needsCartSDKPlugin) Needs() []pluginsdk.Capability {
	return []pluginsdk.Capability{pluginsdk.CapCart}
}

func (needsCartSDKPlugin) Init(h pluginsdk.Host) error {
	h.Require(pluginsdk.CapCart)
	return nil
}

type needsCatalogSeedSDKPlugin struct{}

func (needsCatalogSeedSDKPlugin) Name() string { return "test/needscatalogseed" }

func (needsCatalogSeedSDKPlugin) Needs() []pluginsdk.Capability {
	return []pluginsdk.Capability{pluginsdk.CapCatalogSeed}
}

func (needsCatalogSeedSDKPlugin) Init(h pluginsdk.Host) error {
	h.Require(pluginsdk.CapCatalogSeed)
	return nil
}

// TestNonServeBoot_SkipsCapCartAndCapCatalogSeed exercises the same wiring
// path worker, scheduler, and the cache/schedule/extension-field CLIs use:
// bindProductPorts (no CartPort, no CatalogSeedPort), InitAll, then
// AssertNeedsMetExcept(CapCart, CapCatalogSeed). Plugins that declare those
// Needs must not block this boot path.
func TestNonServeBoot_SkipsCapCartAndCapCatalogSeed(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	reg.Register(pluginsdk.Adapt(needsCartSDKPlugin{}))
	reg.Register(pluginsdk.Adapt(needsCatalogSeedSDKPlugin{}))

	app := &plugin.App{Logger: log}
	if err := bindProductPorts(app, nil); err != nil {
		t.Fatalf("bindProductPorts: %v", err)
	}

	summary := reg.InitAll(app)
	if summary.Failed != 0 {
		t.Fatalf("InitAll summary = %+v, want no failures", summary)
	}

	if err := app.AssertNeedsMetExcept(plugin.CapCart, plugin.CapCatalogSeed); err != nil {
		t.Fatalf("AssertNeedsMetExcept(CapCart, CapCatalogSeed): %v, want nil", err)
	}
	if err := app.AssertNeedsMetExcept(plugin.CapCart); err == nil {
		t.Fatal("AssertNeedsMetExcept(CapCart only): expected CapCatalogSeed unbound error")
	}
	if err := app.AssertNeedsMet(); err == nil {
		t.Fatal("AssertNeedsMet: expected error — CartPort and CatalogSeedPort were never bound")
	}
	if _, ok := app.CatalogSeedPort(); ok {
		t.Fatal("bindProductPorts must not bind CapCatalogSeed")
	}
}
