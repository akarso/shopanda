package main

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/pricing"
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

// TestNonServeBoot_SkipsCapCartAndCapCatalogSeed uses assertWorkerLiteNeedsMet —
// the skip list for CLIs that never bind CapJobs / CapMedia.
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

	if err := assertWorkerLiteNeedsMet(app); err != nil {
		t.Fatalf("assertWorkerLiteNeedsMet: %v, want nil", err)
	}
	if err := assertServeNeedsMet(app); err == nil {
		t.Fatal("assertServeNeedsMet: expected CapCart unbound error")
	}
	if err := app.AssertNeedsMet(); err == nil {
		t.Fatal("AssertNeedsMet: expected error — CartPort and CatalogSeedPort were never bound")
	}
	if _, ok := app.CatalogSeedPort(); ok {
		t.Fatal("bindProductPorts must not bind CapCatalogSeed")
	}
}

type needsJobsSDKPlugin struct{}

func (needsJobsSDKPlugin) Name() string { return "test/needsjobs" }
func (needsJobsSDKPlugin) Needs() []pluginsdk.Capability {
	return []pluginsdk.Capability{pluginsdk.CapJobs}
}
func (needsJobsSDKPlugin) Init(h pluginsdk.Host) error {
	h.Require(pluginsdk.CapJobs)
	return nil
}

func TestWorkerAssert_CapJobsRequiresBindFirst(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	reg.Register(pluginsdk.Adapt(needsJobsSDKPlugin{}))
	app := &plugin.App{Logger: log}
	if err := bindProductPorts(app, nil); err != nil {
		t.Fatal(err)
	}
	if summary := reg.InitAll(app); summary.Failed != 0 {
		t.Fatalf("InitAll: %+v", summary)
	}
	if err := assertWorkerNeedsMet(app); err == nil {
		t.Fatal("assertWorkerNeedsMet must fail before CapJobs bind")
	}
	if err := assertWorkerLiteNeedsMet(app); err != nil {
		t.Fatalf("lite assert should skip CapJobs: %v", err)
	}
	port, err := pluginsdk.NewJobsPort(stubJobsReaderForBoot{}, &stubJobsQueueForBoot{})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.BindJobsPort(port); err != nil {
		t.Fatal(err)
	}
	if err := assertWorkerNeedsMet(app); err != nil {
		t.Fatalf("assertWorkerNeedsMet after BindJobsPort: %v", err)
	}
}

type stubJobsQueueForBoot struct{}

func (stubJobsQueueForBoot) Enqueue(context.Context, jobs.Job) error        { return nil }
func (stubJobsQueueForBoot) Dequeue(context.Context) (*jobs.Job, error)     { return nil, nil }
func (stubJobsQueueForBoot) Complete(context.Context, string, int) error    { return nil }
func (stubJobsQueueForBoot) Fail(context.Context, string, int, error) error { return nil }
func (stubJobsQueueForBoot) Requeue(context.Context, string, int, ...jobs.RequeueOption) error {
	return nil
}

type stubJobsReaderForBoot struct{}

func (stubJobsReaderForBoot) List(context.Context, jobs.ListFilter) ([]jobs.Summary, error) {
	return nil, nil
}
func (stubJobsReaderForBoot) Get(context.Context, string) (*jobs.Detail, error) { return nil, nil }
func (stubJobsReaderForBoot) CountsByStatus(context.Context) (map[jobs.Status]int, error) {
	return map[jobs.Status]int{}, nil
}
func (stubJobsReaderForBoot) CountsByType(context.Context, string) (map[jobs.Status]int, error) {
	return nil, nil
}

func TestBindCatalogSeedPort_NilBootFailsClosed(t *testing.T) {
	app := &plugin.App{}
	if err := bindCatalogSeedPort(app, nil); err == nil {
		t.Fatal("expected error for nil bootstrap")
	}
	if _, ok := app.CatalogSeedPort(); ok {
		t.Fatal("nil boot must not bind CapCatalogSeed")
	}
}

func TestSeedBoot_BindsCatalogSeed(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	reg.Register(pluginsdk.Adapt(needsCatalogSeedSDKPlugin{}))

	app := &plugin.App{Logger: log}
	boot := &plugin.Bootstrap{
		Products:     seedBootProducts{},
		Variants:     seedBootVariants{},
		Prices:       seedBootPrices{},
		PriceHistory: seedBootHistory{},
		Stock:        seedBootStock{},
	}
	if err := bindProductPorts(app, boot); err != nil {
		t.Fatalf("bindProductPorts: %v", err)
	}
	if err := bindCatalogSeedPort(app, boot); err != nil {
		t.Fatalf("bindCatalogSeedPort: %v", err)
	}

	summary := reg.InitAll(app)
	if summary.Failed != 0 {
		t.Fatalf("InitAll summary = %+v, want no failures", summary)
	}

	if err := assertSeedCLINeedsMet(app); err != nil {
		t.Fatalf("assertSeedCLINeedsMet: %v", err)
	}
	if _, ok := app.CatalogSeedPort(); !ok {
		t.Fatal("CatalogSeedPort must be bound after bindCatalogSeedPort")
	}
}

func TestServeBoot_SkipsCatalogSeedUnbound(t *testing.T) {
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
		t.Fatalf("InitAll summary = %+v", summary)
	}
	if err := app.BindCartPort(struct{}{}); err != nil {
		t.Fatalf("BindCartPort: %v", err)
	}
	if err := assertServeNeedsMet(app); err != nil {
		t.Fatalf("assertServeNeedsMet: %v", err)
	}
	if _, ok := app.CatalogSeedPort(); ok {
		t.Fatal("serve path must leave CatalogSeedPort unbound")
	}
	if err := app.AssertNeedsMet(); err == nil {
		t.Fatal("AssertNeedsMet must still fail while CapCatalogSeed is unbound")
	}
}

// Minimal repos for NewCatalogSeedPort (nil db — no WithTx required).

type seedBootProducts struct{}

func (seedBootProducts) FindByID(context.Context, string) (*catalog.Product, error) {
	return nil, nil
}
func (seedBootProducts) FindBySlug(context.Context, string) (*catalog.Product, error) {
	return nil, nil
}
func (seedBootProducts) List(context.Context, catalog.ListFilter) ([]catalog.Product, error) {
	return nil, nil
}
func (seedBootProducts) FindByCategoryID(context.Context, string, int, int) ([]catalog.Product, error) {
	return nil, nil
}
func (seedBootProducts) Create(context.Context, *catalog.Product) error { return nil }
func (seedBootProducts) Update(context.Context, *catalog.Product) error { return nil }

type seedBootVariants struct{}

func (seedBootVariants) FindByID(context.Context, string) (*catalog.Variant, error) {
	return nil, nil
}
func (seedBootVariants) FindBySKU(context.Context, string) (*catalog.Variant, error) {
	return nil, nil
}
func (seedBootVariants) FindBySKUs(context.Context, []string) (map[string]*catalog.Variant, error) {
	return nil, nil
}
func (seedBootVariants) ListByProductID(context.Context, string, int, int) ([]catalog.Variant, error) {
	return nil, nil
}
func (seedBootVariants) ListByProductIDs(context.Context, []string, int) (map[string][]catalog.Variant, error) {
	return nil, nil
}
func (seedBootVariants) Create(context.Context, *catalog.Variant) error { return nil }
func (seedBootVariants) Update(context.Context, *catalog.Variant) error { return nil }

type seedBootPrices struct{}

func (seedBootPrices) FindByVariantCurrencyAndStore(context.Context, string, string, string) (*pricing.Price, error) {
	return nil, nil
}
func (seedBootPrices) FindByVariantsCurrencyAndStore(context.Context, []string, string, string) (map[string]*pricing.Price, error) {
	return nil, nil
}
func (seedBootPrices) ListByVariantID(context.Context, string) ([]pricing.Price, error) {
	return nil, nil
}
func (seedBootPrices) List(context.Context, int, int) ([]pricing.Price, error) { return nil, nil }
func (seedBootPrices) Upsert(context.Context, *pricing.Price) error            { return nil }

type seedBootHistory struct{}

func (seedBootHistory) Record(context.Context, *pricing.PriceSnapshot) error { return nil }
func (seedBootHistory) LowestSince(context.Context, string, string, string, time.Time) (*pricing.PriceSnapshot, error) {
	return nil, nil
}
func (seedBootHistory) LowestSinceByVariants(context.Context, []string, string, string, time.Time) (map[string]*pricing.PriceSnapshot, error) {
	return nil, nil
}

type seedBootStock struct{}

func (seedBootStock) GetStock(context.Context, string) (inventory.StockEntry, error) {
	return inventory.StockEntry{}, nil
}
func (seedBootStock) SetStock(context.Context, *inventory.StockEntry) error { return nil }
func (seedBootStock) SetStocks(context.Context, []inventory.StockEntry) error {
	return nil
}
func (seedBootStock) ListStock(context.Context, int, int) ([]inventory.StockEntry, error) {
	return nil, nil
}
func (seedBootStock) ListInventory(context.Context, int, int, string) ([]inventory.InventoryListItem, error) {
	return nil, nil
}
func (seedBootStock) GetInventoryItem(context.Context, string) (inventory.InventoryListItem, error) {
	return inventory.InventoryListItem{}, nil
}
