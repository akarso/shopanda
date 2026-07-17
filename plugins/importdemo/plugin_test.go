package importdemo_test

import (
	"context"
	"io"
	"strings"
	"testing"

	importctx "github.com/akarso/shopanda/internal/application/importctx"
	"github.com/akarso/shopanda/internal/application/importer"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/plugins/importdemo"
)

func testApp(cfg *config.Config) *plugin.App {
	log := logger.NewWithWriter(io.Discard, "error")
	return &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: cfg,
	}
}

func initImportDemoPlugin(t *testing.T) *importctx.Registry {
	t.Helper()
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			ImportDemo: config.ImportDemoPluginConfig{Enabled: true},
		},
	}
	importReg := importctx.NewRegistry(nil)
	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	reg.Register(importdemo.New())
	app := testApp(cfg)
	app.SetImportRegistry(importReg)
	if summary := reg.InitAll(app); summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll() summary = %+v", summary)
	}
	return importReg
}

func TestPlugin_Name(t *testing.T) {
	if got := importdemo.New().Name(); got != "importdemo/reference" {
		t.Fatalf("Name() = %q, want importdemo/reference", got)
	}
}

func TestPlugin_Init_DisabledReturnsError(t *testing.T) {
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			ImportDemo: config.ImportDemoPluginConfig{Enabled: false},
		},
	}
	if err := importdemo.New().Init(testApp(cfg)); err == nil {
		t.Fatal("Init() expected error when disabled")
	}
}

func TestPlugin_Init_RegistersProductRowHook(t *testing.T) {
	importReg := initImportDemoPlugin(t)
	catalog := importReg.Catalog()
	if len(catalog) != 1 || catalog[0].Entity != importctx.EntityProduct {
		t.Fatalf("catalog = %+v", catalog)
	}
	if catalog[0].Hook != "import.product.row" {
		t.Fatalf("hook = %q", catalog[0].Hook)
	}
}

func TestRemapProductRow_MapsERPColumns(t *testing.T) {
	ctx := &extapi.ImportRowContext{
		RowIndex: 2,
		Row: map[string]string{
			"matnr":  "SKU-001",
			"maktx":  "Widget",
			"maktx2": "A fine widget",
		},
	}
	importdemo.RemapProductRow(ctx)
	if ctx.Row["sku"] != "SKU-001" || ctx.Row["name"] != "Widget" || ctx.Row["description"] != "A fine widget" {
		t.Fatalf("row = %v", ctx.Row)
	}
	if ctx.Row["slug"] != "sku-001" {
		t.Fatalf("slug = %q, want sku-001", ctx.Row["slug"])
	}
	if _, ok := ctx.Row["matnr"]; ok {
		t.Fatal("expected matnr removed after remap")
	}
}

func TestRemapProductRow_MissingSKUAppendsError(t *testing.T) {
	ctx := &extapi.ImportRowContext{RowIndex: 3, Row: map[string]string{"maktx": "Widget"}}
	importdemo.RemapProductRow(ctx)
	if len(ctx.Errors) != 1 || ctx.Errors[0].Code != importdemo.ValidationCodeMissingSKU {
		t.Fatalf("errors = %+v", ctx.Errors)
	}
}

type mockProductRepo struct {
	products []*catalog.Product
}

func (m *mockProductRepo) FindByID(_ context.Context, _ string) (*catalog.Product, error) {
	return nil, nil
}
func (m *mockProductRepo) FindBySlug(_ context.Context, _ string) (*catalog.Product, error) {
	return nil, nil
}
func (m *mockProductRepo) List(_ context.Context, _, _ int) ([]catalog.Product, error) {
	return nil, nil
}
func (m *mockProductRepo) Create(_ context.Context, p *catalog.Product) error {
	m.products = append(m.products, p)
	return nil
}
func (m *mockProductRepo) Update(_ context.Context, _ *catalog.Product) error { return nil }
func (m *mockProductRepo) FindByCategoryID(_ context.Context, _ string, _, _ int) ([]catalog.Product, error) {
	return nil, nil
}

type mockVariantRepo struct {
	variants []*catalog.Variant
}

func (m *mockVariantRepo) FindByID(_ context.Context, _ string) (*catalog.Variant, error) {
	return nil, nil
}
func (m *mockVariantRepo) FindBySKU(_ context.Context, _ string) (*catalog.Variant, error) {
	return nil, nil
}
func (m *mockVariantRepo) FindBySKUs(_ context.Context, _ []string) (map[string]*catalog.Variant, error) {
	return map[string]*catalog.Variant{}, nil
}
func (m *mockVariantRepo) ListByProductID(_ context.Context, _ string, _, _ int) ([]catalog.Variant, error) {
	return nil, nil
}
func (m *mockVariantRepo) ListByProductIDs(_ context.Context, _ []string, _ int) (map[string][]catalog.Variant, error) {
	return nil, nil
}
func (m *mockVariantRepo) Create(_ context.Context, v *catalog.Variant) error {
	m.variants = append(m.variants, v)
	return nil
}
func (m *mockVariantRepo) Update(_ context.Context, _ *catalog.Variant) error { return nil }

func TestProductImport_ERPCSVWithPluginRemap(t *testing.T) {
	csv := `matnr,maktx,maktx2
SKU-001,Widget,A fine widget
`
	importReg := initImportDemoPlugin(t)
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil).WithRowHooks(importReg)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 1 || result.Variants != 1 || result.Skipped != 0 {
		t.Fatalf("result = %+v", result)
	}
	if len(prodRepo.products) != 1 || prodRepo.products[0].Slug != "sku-001" {
		t.Fatalf("products = %+v", prodRepo.products)
	}
	if len(varRepo.variants) != 1 || varRepo.variants[0].SKU != "SKU-001" {
		t.Fatalf("variants = %+v", varRepo.variants)
	}
}

func TestProductImport_MissingMATNRFailsRow(t *testing.T) {
	csv := `matnr,maktx
,Widget
`
	importReg := initImportDemoPlugin(t)
	imp := importer.NewProductImporter(&mockProductRepo{}, &mockVariantRepo{}, nil).WithRowHooks(importReg)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Skipped != 1 || result.Products != 0 {
		t.Fatalf("result = %+v", result)
	}
	if len(result.RowErrors) != 1 || result.RowErrors[0].Code != importdemo.ValidationCodeMissingSKU {
		t.Fatalf("rowErrors = %+v", result.RowErrors)
	}
}
