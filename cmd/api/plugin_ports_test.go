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

// TestNonServeBoot_SkipsCapCart exercises the same wiring path worker,
// scheduler, and the cache/schedule/extension-field CLIs use: bindProductPorts
// (no CartPort bind — only serve constructs cart.Service), InitAll, then
// AssertNeedsMetExcept(CapCart). A plugin that declares Needs(CapCart) (like
// plugins/extref) must not block this boot path, while AssertNeedsMet (the
// serve-only check) must still fail closed for the same App.
func TestNonServeBoot_SkipsCapCart(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	reg.Register(pluginsdk.Adapt(needsCartSDKPlugin{}))

	app := &plugin.App{Logger: log}
	// No DB bootstrap: mirrors a CLI/worker path that never calls
	// bindProductPorts with a real *plugin.Bootstrap, let alone BindCartPort.
	if err := bindProductPorts(app, nil); err != nil {
		t.Fatalf("bindProductPorts: %v", err)
	}

	summary := reg.InitAll(app)
	if summary.Failed != 0 {
		t.Fatalf("InitAll summary = %+v, want no failures", summary)
	}

	if err := app.AssertNeedsMetExcept(plugin.CapCart); err != nil {
		t.Fatalf("AssertNeedsMetExcept(CapCart): %v, want nil on a non-serve boot path", err)
	}
	if err := app.AssertNeedsMet(); err == nil {
		t.Fatal("AssertNeedsMet: expected error — CartPort was never bound")
	}
}
