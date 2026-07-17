package taxdemo_test

import (
	"context"
	"io"
	"testing"

	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/taxdemo"
)

func testApp(cfg *config.Config) *plugin.App {
	log := logger.NewWithWriter(io.Discard, "error")
	return &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: cfg,
	}
}

func initTaxDemoPlugin(t *testing.T, rateBPS int) *plugin.App {
	t.Helper()
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			TaxDemo: config.TaxDemoPluginConfig{
				Enabled:     true,
				FlatRateBPS: rateBPS,
			},
		},
	}
	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	reg.Register(taxdemo.New())
	app := testApp(cfg)
	if summary := reg.InitAll(app); summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll() summary = %+v", summary)
	}
	return app
}

func taxPricingContext(t *testing.T, currency, country, mode string, amount int64) pricing.PricingContext {
	t.Helper()
	pctx, err := pricing.NewPricingContext(currency)
	if err != nil {
		t.Fatal(err)
	}
	item, err := pricing.NewPricingItem("v1", 1, shared.MustNewMoney(amount, currency))
	if err != nil {
		t.Fatal(err)
	}
	pctx.Items = []pricing.PricingItem{item}
	pctx.Meta["tax_country"] = country
	pctx.Meta["tax_mode"] = mode
	return pctx
}

func TestPlugin_Name(t *testing.T) {
	if got := taxdemo.New().Name(); got != "taxdemo/reference" {
		t.Fatalf("Name() = %q, want taxdemo/reference", got)
	}
}

func TestPlugin_Init_DisabledReturnsError(t *testing.T) {
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			TaxDemo: config.TaxDemoPluginConfig{Enabled: false},
		},
	}
	if err := taxdemo.New().Init(testApp(cfg)); err == nil {
		t.Fatal("Init() expected error when disabled")
	}
}

func TestPlugin_Init_RegistersCalculatorAndReplaceStep(t *testing.T) {
	app := initTaxDemoPlugin(t, 2100)

	if _, ok := app.TaxCalculator(); !ok {
		t.Fatal("TaxCalculator() ok = false, want true")
	}

	regs := app.PricingStepRegistrations()
	if len(regs) != 1 {
		t.Fatalf("pricing registrations = %d, want 1", len(regs))
	}
	if regs[0].Position != "replace:tax" {
		t.Fatalf("position = %q, want replace:tax", regs[0].Position)
	}
	step, ok := regs[0].Step.(pricing.PricingStep)
	if !ok {
		t.Fatalf("step type = %T", regs[0].Step)
	}
	if step.Name() != taxdemo.TaxStepName {
		t.Fatalf("step name = %q", step.Name())
	}
}

func TestFlatRateCalculator_Exclusive(t *testing.T) {
	calc := taxdemo.NewFlatRateCalculator(2000)
	pctx := taxPricingContext(t, "EUR", "DE", "exclusive", 10000)

	if err := calc.Calculate(context.Background(), &pctx); err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if len(pctx.Items[0].Adjustments) != 1 {
		t.Fatalf("adjustments = %d, want 1", len(pctx.Items[0].Adjustments))
	}
	if got := pctx.Items[0].Adjustments[0].Amount.Amount(); got != 2000 {
		t.Fatalf("tax amount = %d, want 2000", got)
	}
	if pctx.Items[0].Adjustments[0].Code != taxdemo.AdjustmentCode {
		t.Fatalf("code = %q", pctx.Items[0].Adjustments[0].Code)
	}
}

func TestFlatRateCalculator_NoOpWithoutMeta(t *testing.T) {
	calc := taxdemo.NewFlatRateCalculator(2000)
	pctx, err := pricing.NewPricingContext("EUR")
	if err != nil {
		t.Fatal(err)
	}
	item, err := pricing.NewPricingItem("v1", 1, shared.MustNewMoney(10000, "EUR"))
	if err != nil {
		t.Fatal(err)
	}
	pctx.Items = []pricing.PricingItem{item}

	if err := calc.Calculate(context.Background(), &pctx); err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if len(pctx.Items[0].Adjustments) != 0 {
		t.Fatalf("adjustments = %d, want 0", len(pctx.Items[0].Adjustments))
	}
}

func TestTaxStep_ReplacesCoreTaxInMergedPipeline(t *testing.T) {
	app := initTaxDemoPlugin(t, 1900)

	core := []pricing.PricingStep{
		&stubStep{name: "base"},
		&stubStep{name: "catalog_promotions"},
		&stubStep{name: "cart_promotions"},
		appPricing.NewTaxStep(taxdemo.NewFlatRateCalculator(1900)),
		pricing.NewFinalizeStep(),
	}
	regs := make([]appPricing.PluginStepRegistration, 0, len(app.PricingStepRegistrations()))
	for _, reg := range app.PricingStepRegistrations() {
		step, ok := reg.Step.(pricing.PricingStep)
		if !ok {
			continue
		}
		regs = append(regs, appPricing.PluginStepRegistration{Step: step, Position: reg.Position})
	}
	steps, err := appPricing.MergePluginSteps(core, regs)
	if err != nil {
		t.Fatalf("MergePluginSteps: %v", err)
	}

	names := make([]string, len(steps))
	for i, step := range steps {
		names[i] = step.Name()
	}
	want := []string{"base", "catalog_promotions", "cart_promotions", taxdemo.TaxStepName, "finalize"}
	if len(names) != len(want) {
		t.Fatalf("pipeline = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("pipeline = %v, want %v", names, want)
		}
	}

	pctx := taxPricingContext(t, "EUR", "DE", "exclusive", 10000)
	if err := pricing.NewPipeline(steps...).Execute(context.Background(), &pctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := pctx.Items[0].Adjustments[0].Amount.Amount(); got != 1900 {
		t.Fatalf("tax amount = %d, want 1900", got)
	}
}

type stubStep struct {
	name string
}

func (s stubStep) Name() string { return s.name }

func (s stubStep) Apply(_ context.Context, _ *pricing.PricingContext) error { return nil }
