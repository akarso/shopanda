package example_test

import (
	"context"
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/example"
)

func testApp(cfg *config.Config) *plugin.App {
	log := logger.NewWithWriter(io.Discard, "error")
	return &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: cfg,
	}
}

func TestPlugin_Name(t *testing.T) {
	if got := example.New().Name(); got != "example/demo" {
		t.Fatalf("Name() = %q, want example/demo", got)
	}
}

func TestPlugin_Init_DisabledConfigReturnsError(t *testing.T) {
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			Example: config.ExamplePluginConfig{Enabled: false},
		},
	}
	if err := example.New().Init(testApp(cfg)); err == nil {
		t.Fatal("Init() expected error when plugins.example.enabled is false")
	}
}

func TestPlugin_Init_RegistersPricingStepPermissionAndListener(t *testing.T) {
	t.Cleanup(rbac.ResetPluginPermissions)

	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			Example: config.ExamplePluginConfig{Enabled: true, FeeMinorUnits: 100},
		},
	}
	app := testApp(cfg)
	if err := example.New().Init(app); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	steps := app.PricingSteps()
	if len(steps) != 1 {
		t.Fatalf("PricingSteps() len = %d, want 1", len(steps))
	}
	step, ok := steps[0].(pricing.PricingStep)
	if !ok {
		t.Fatalf("PricingSteps()[0] type = %T, want pricing.PricingStep", steps[0])
	}
	if step.Name() != "example.fee" {
		t.Fatalf("step name = %q, want example.fee", step.Name())
	}

	if app.Bus.Handlers(order.EventOrderCreated) != 1 {
		t.Fatalf("order.created handlers = %d, want 1", app.Bus.Handlers(order.EventOrderCreated))
	}

	if !rbac.HasPermission(identity.RoleAdmin, example.PermissionReportsRead) {
		t.Fatal("expected example.reports.read for admin role")
	}
}

func TestExampleFeeStep_AddsFeeToPipeline(t *testing.T) {
	step := example.NewExampleFeeStep(100)
	pctx, err := pricing.NewPricingContext("EUR")
	if err != nil {
		t.Fatalf("NewPricingContext: %v", err)
	}
	unit, err := shared.NewMoney(1000, "EUR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	item, err := pricing.NewPricingItem("v1", 1, unit)
	if err != nil {
		t.Fatalf("NewPricingItem: %v", err)
	}
	pctx.Items = []pricing.PricingItem{item}

	pipe := pricing.NewPipeline(step, pricing.NewFinalizeStep())
	if err := pipe.Execute(context.Background(), &pctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if pctx.FeesTotal.Amount() != 100 {
		t.Fatalf("FeesTotal = %d, want 100", pctx.FeesTotal.Amount())
	}
	if pctx.GrandTotal.Amount() != 1100 {
		t.Fatalf("GrandTotal = %d, want 1100", pctx.GrandTotal.Amount())
	}
}
