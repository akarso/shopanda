package plugin_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
)

type fakeCatalogPort struct{}

func (fakeCatalogPort) FindVariant(context.Context, string) (*extapi.CatalogVariant, error) {
	return nil, nil
}
func (fakeCatalogPort) FindVariants(context.Context, []string) (map[string]*extapi.CatalogVariant, error) {
	return nil, nil
}
func (fakeCatalogPort) FindProduct(context.Context, string) (*extapi.CatalogProduct, error) {
	return nil, nil
}

func TestAssertNeedsMet_CatalogSatisfied(t *testing.T) {
	app := &plugin.App{}
	app.RecordNeeds(plugin.CapCatalog, plugin.CapDB)
	app.MarkCapabilitySatisfied(plugin.CapDB)
	if err := app.BindCatalogPort(fakeCatalogPort{}); err != nil {
		t.Fatalf("BindCatalogPort: %v", err)
	}
	if err := app.AssertNeedsMet(); err != nil {
		t.Fatalf("AssertNeedsMet: %v", err)
	}
}

func TestAssertNeedsMet_MissingCart(t *testing.T) {
	app := &plugin.App{}
	app.RecordNeeds(plugin.CapCart)
	if err := app.AssertNeedsMet(); err == nil {
		t.Fatal("expected error for unbound CapCart")
	}
}

// TestAssertNeedsMetExcept_SkipsCart simulates a non-serve composition root
// (worker/scheduler/CLI): a plugin declares Needs(CapCart) but the process
// never binds CartPort. AssertNeedsMetExcept(CapCart) must succeed while
// plain AssertNeedsMet still fails closed for the same App.
func TestAssertNeedsMetExcept_SkipsCart(t *testing.T) {
	app := &plugin.App{}
	app.RecordNeeds(plugin.CapCart)
	if err := app.AssertNeedsMetExcept(plugin.CapCart); err != nil {
		t.Fatalf("AssertNeedsMetExcept(CapCart): %v", err)
	}
	if err := app.AssertNeedsMet(); err == nil {
		t.Fatal("AssertNeedsMet: expected error for unbound CapCart")
	}
}

func TestAssertNeedsMetExcept_SkipsCatalogSeed(t *testing.T) {
	app := &plugin.App{}
	app.RecordNeeds(plugin.CapCatalogSeed)
	if err := app.AssertNeedsMetExcept(plugin.CapCatalogSeed); err != nil {
		t.Fatalf("AssertNeedsMetExcept(CapCatalogSeed): %v", err)
	}
	if err := app.AssertNeedsMetExcept(plugin.CapCart, plugin.CapCatalogSeed); err != nil {
		t.Fatalf("AssertNeedsMetExcept(CapCart, CapCatalogSeed): %v", err)
	}
	if err := app.AssertNeedsMet(); err == nil {
		t.Fatal("AssertNeedsMet: expected error for unbound CapCatalogSeed")
	}
}

// TestAssertNeedsMetExcept_OtherCapsStillEnforced confirms the skip list is
// scoped to the named capabilities only — a different unmet Need still fails.
func TestAssertNeedsMetExcept_OtherCapsStillEnforced(t *testing.T) {
	app := &plugin.App{}
	app.RecordNeeds(plugin.CapCart, plugin.CapOrders)
	if err := app.AssertNeedsMetExcept(plugin.CapCart); err == nil {
		t.Fatal("expected error for unbound CapOrders")
	}
}

// TestAssertNeedsMet_ErrorNamesPlugin confirms the error names the plugin
// that declared the unmet capability.
func TestAssertNeedsMet_ErrorNamesPlugin(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	reg.Register(&needsCartPlugin{name: "acme/needscart"})
	app := &plugin.App{Logger: log}
	summary := reg.InitAll(app)
	if summary.Failed != 0 {
		t.Fatalf("InitAll summary = %+v", summary)
	}
	err := app.AssertNeedsMet()
	if err == nil {
		t.Fatal("expected error for unbound CapCart")
	}
	if !strings.Contains(err.Error(), "acme/needscart") {
		t.Fatalf("error = %v, want plugin name in message", err)
	}
}

// TestRecordNeeds_NoOpAfterFreeze confirms a late Require (e.g. a handler
// closure outliving Init) does not poison the required-capability set after
// FreezePluginNeeds has already run.
func TestRecordNeeds_NoOpAfterFreeze(t *testing.T) {
	app := &plugin.App{}
	app.FreezePluginNeeds()
	app.RecordNeeds(plugin.CapOrders)
	if err := app.AssertNeedsMet(); err != nil {
		t.Fatalf("AssertNeedsMet: %v, want nil (RecordNeeds after freeze should no-op)", err)
	}
}

type needsCartPlugin struct{ name string }

func (p *needsCartPlugin) Name() string               { return p.name }
func (p *needsCartPlugin) Needs() []plugin.Capability { return []plugin.Capability{plugin.CapCart} }
func (p *needsCartPlugin) Init(app *plugin.App) error {
	return nil
}
