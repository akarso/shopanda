package cartdemo_test

import (
	"context"
	"io"
	"testing"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	"github.com/akarso/shopanda/internal/application/hooks"
	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/plugins/cartdemo"
)

func testApp(cfg *config.Config) *plugin.App {
	log := logger.NewWithWriter(io.Discard, "error")
	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: cfg,
	}
	app.Hooks("test")
	return app
}

func initCartDemoPlugin(t *testing.T, minQty int, feeMinor int64) (*plugin.App, *hooks.Registry) {
	t.Helper()
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			CartDemo: config.CartDemoPluginConfig{
				Enabled:               true,
				MinQuantity:           minQty,
				HandlingFeeMinorUnits: feeMinor,
			},
		},
	}
	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	reg.Register(cartdemo.New())
	app := testApp(cfg)
	if summary := reg.InitAll(app); summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll() summary = %+v", summary)
	}
	return app, app.HookRegistry()
}

func TestPlugin_Name(t *testing.T) {
	if got := cartdemo.New().Name(); got != "cartdemo/reference" {
		t.Fatalf("Name() = %q, want cartdemo/reference", got)
	}
}

func TestPlugin_Init_DisabledReturnsError(t *testing.T) {
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			CartDemo: config.CartDemoPluginConfig{Enabled: false},
		},
	}
	if err := cartdemo.New().Init(testApp(cfg)); err == nil {
		t.Fatal("Init() expected error when disabled")
	}
}

func TestPlugin_Init_RegistersValidateHookAndPricingStep(t *testing.T) {
	app, hookReg := initCartDemoPlugin(t, 3, 75)

	catalog := hookReg.Catalog()
	if len(catalog) != 1 || catalog[0].Name != string(extapi.HookCartValidate) {
		t.Fatalf("hook catalog = %+v", catalog)
	}

	regs := app.PricingStepRegistrations()
	if len(regs) != 1 {
		t.Fatalf("pricing registrations = %d, want 1", len(regs))
	}
	if regs[0].Position != "after:promotions" {
		t.Fatalf("position = %q, want after:promotions", regs[0].Position)
	}
	step, ok := regs[0].Step.(pricing.PricingStep)
	if !ok {
		t.Fatalf("step type = %T", regs[0].Step)
	}
	if step.Name() != cartdemo.HandlingFeeStepName {
		t.Fatalf("step name = %q", step.Name())
	}
}

func TestHandlingFeeStep_AddsFeeToPipeline(t *testing.T) {
	fee := int64(75)
	step := cartdemo.NewHandlingFeeStep(&fee)
	pctx, err := pricing.NewPricingContext("EUR")
	if err != nil {
		t.Fatalf("NewPricingContext: %v", err)
	}
	unit, err := shared.NewMoney(1000, "EUR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	item, err := pricing.NewPricingItem("v1", 2, unit)
	if err != nil {
		t.Fatalf("NewPricingItem: %v", err)
	}
	pctx.Items = []pricing.PricingItem{item}

	pipe := pricing.NewPipeline(step, pricing.NewFinalizeStep())
	if err := pipe.Execute(context.Background(), &pctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if pctx.FeesTotal.Amount() != 75 {
		t.Fatalf("FeesTotal = %d, want 75", pctx.FeesTotal.Amount())
	}
	if pctx.GrandTotal.Amount() != 2075 {
		t.Fatalf("GrandTotal = %d, want 2075", pctx.GrandTotal.Amount())
	}
}

func TestValidateHook_AppendsIssueBelowMinQuantity(t *testing.T) {
	_, hookReg := initCartDemoPlugin(t, 2, 50)

	c, err := domainCart.NewCart("cart-1", "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.AddItem("var-1", 1, shared.MustNewMoney(1000, "EUR")); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	hookCtx := hooks.NewContext(hooks.HookCartValidate)
	issues := []extapi.CartValidationIssue{}
	hookCtx.Set("cart", &c)
	hookCtx.Set("validation_errors", &issues)
	if err := hookReg.Invoke(context.Background(), hookCtx); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	got := hooks.ValidationIssuesFromContext(hookCtx)
	if len(got) != 1 || got[0].Code != cartdemo.ValidationCodeMinQuantity {
		t.Fatalf("issues = %+v", got)
	}
}

func TestCartService_MinQuantityBlocksAdd(t *testing.T) {
	_, hookReg := initCartDemoPlugin(t, 2, 50)
	svc, prices := newCartServiceWithHooks(t, hookReg)

	ctx := context.Background()
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	prices.set("var-1", "EUR", 1000)

	if _, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{}); err == nil {
		t.Fatal("expected add with qty 1 to fail validation")
	} else if !cartApp.IsValidationFailed(err) {
		t.Fatalf("err = %v, want ValidationFailed", err)
	}

	updated, err := svc.GetCart(ctx, c.ID, "cust-1")
	if err != nil {
		t.Fatalf("GetCart: %v", err)
	}
	if len(updated.Items) != 0 {
		t.Fatalf("items = %d, want 0", len(updated.Items))
	}
}

func TestCartService_ValidQuantityPersistsWithHandlingFeePipeline(t *testing.T) {
	app, hookReg := initCartDemoPlugin(t, 2, 50)
	svc, prices := newCartServiceWithHooksAndPluginSteps(t, hookReg, app)

	ctx := context.Background()
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	prices.set("var-1", "EUR", 1000)

	updated, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 2, cartApp.AddItemOptions{})
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if len(updated.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(updated.Items))
	}

	issues, err := svc.ValidationIssues(ctx, updated)
	if err != nil {
		t.Fatalf("ValidationIssues: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("validation issues = %+v, want none", issues)
	}

	pctx, err := pricing.NewPricingContext("EUR")
	if err != nil {
		t.Fatalf("NewPricingContext: %v", err)
	}
	unit := updated.Items[0].UnitPrice
	item, err := pricing.NewPricingItem("var-1", 2, unit)
	if err != nil {
		t.Fatalf("NewPricingItem: %v", err)
	}
	pctx.Items = []pricing.PricingItem{item}
	pipe := buildCartDemoPipeline(prices, app)
	if err := pipe.Execute(ctx, &pctx); err != nil {
		t.Fatalf("pipeline Execute: %v", err)
	}
	if pctx.FeesTotal.Amount() != 50 {
		t.Fatalf("FeesTotal = %d, want 50", pctx.FeesTotal.Amount())
	}
}
