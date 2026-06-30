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
	rbac.ResetPluginPermissions()
	t.Cleanup(rbac.ResetPluginPermissions)

	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			Example: config.ExamplePluginConfig{Enabled: true, FeeMinorUnits: 100},
		},
	}
	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	reg.Register(example.New())
	app := testApp(cfg)
	if summary := reg.InitAll(app); summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll() summary = %+v, want 1 initialized and 0 failed", summary)
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

func TestPlugin_Init_RegistersCLICommand(t *testing.T) {
	rbac.ResetPluginPermissions()
	t.Cleanup(rbac.ResetPluginPermissions)

	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	reg.Register(example.New())
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			Example: config.ExamplePluginConfig{Enabled: true},
		},
	}
	reg.InitAll(testApp(cfg))

	cmds := reg.CLIRegistry().List()
	if len(cmds) != 1 || cmds[0].Name != example.CommandPing {
		t.Fatalf("CLI commands = %#v, want %q", cmds, example.CommandPing)
	}
}

func TestPlugin_Init_RegistersAdminConfig(t *testing.T) {
	rbac.ResetPluginPermissions()
	t.Cleanup(rbac.ResetPluginPermissions)

	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	reg.Register(example.New())
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			Example: config.ExamplePluginConfig{Enabled: true, FeeMinorUnits: 100},
		},
	}
	app := testApp(cfg)
	summary := reg.InitAll(app)
	if summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll() summary = %+v, want 1 initialized and 0 failed", summary)
	}
	keys := reg.ConfigRegistry().Keys()
	if len(keys) != 1 || keys[0] != "plugins.example.fee_minor_units" {
		t.Fatalf("ConfigRegistry keys = %v, want [plugins.example.fee_minor_units]", keys)
	}
}

func TestExampleFeeStep_AddsFeeToPipeline(t *testing.T) {
	fee := int64(100)
	step := example.NewExampleFeeStep(&fee)
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

func TestExampleFeeStep_ReflectsRuntimeConfigChange(t *testing.T) {
	fee := int64(100)
	step := example.NewExampleFeeStep(&fee)
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
		t.Fatalf("first Execute: %v", err)
	}
	if pctx.FeesTotal.Amount() != 100 {
		t.Fatalf("FeesTotal after first run = %d, want 100", pctx.FeesTotal.Amount())
	}

	fee = 250
	pctx, err = pricing.NewPricingContext("EUR")
	if err != nil {
		t.Fatalf("NewPricingContext: %v", err)
	}
	pctx.Items = []pricing.PricingItem{item}
	if err := pipe.Execute(context.Background(), &pctx); err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if pctx.FeesTotal.Amount() != 250 {
		t.Fatalf("FeesTotal after second run = %d, want 250", pctx.FeesTotal.Amount())
	}
}
