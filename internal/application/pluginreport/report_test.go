package pluginreport_test

import (
	"context"
	"strings"
	"testing"

	hooksapp "github.com/akarso/shopanda/internal/application/hooks"
	importctxapp "github.com/akarso/shopanda/internal/application/importctx"
	"github.com/akarso/shopanda/internal/application/pluginreport"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/plugins/cartdemo"
)

type namedStep struct{ name string }

func (s namedStep) Name() string { return s.name }

func TestBuild_IncludesPluginPricingAndHooks(t *testing.T) {
	log := logger.New("error")
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			CartDemo: config.CartDemoPluginConfig{Enabled: true},
		},
	}
	registry := plugin.NewRegistry(log)
	registry.Register(cartdemo.New())
	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: cfg,
	}
	app.SetHookRegistry(hooksapp.NewRegistry(log))
	app.SetImportRegistry(importctxapp.NewRegistry(log))
	if summary := registry.InitAll(app); summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll() = %+v", summary)
	}

	report := pluginreport.Build(registry, app, cfg)
	if len(report.Plugins) != 1 || report.Plugins[0].State != "active" {
		t.Fatalf("plugins = %+v", report.Plugins)
	}
	if len(report.PricingSteps) != 1 || report.PricingSteps[0].Name != "cartdemo.handling_fee" {
		t.Fatalf("pricing = %+v", report.PricingSteps)
	}
	if len(report.Hooks) == 0 {
		t.Fatal("expected hooks")
	}
	if len(report.CorePricingSteps) == 0 {
		t.Fatal("expected core pricing catalog")
	}
}

func TestFormatText_IncludesSections(t *testing.T) {
	text := pluginreport.FormatText(pluginreport.Report{
		Plugins:          []pluginreport.PluginStatus{{Name: "cartdemo/reference", State: "active"}},
		CorePricingSteps: []string{"base", "tax"},
		PricingSteps:     []pluginreport.PricingStep{{Position: "after:promotions", Name: "fee", Type: "cartdemo.HandlingFeeStep"}},
		SyncJobs: []pluginreport.SyncJob{{
			PluginSlug: "warehousedemo",
			JobType:    "integration.sync.warehousedemo.warehouse.stock",
			Trigger:    "cron",
			Detail:     "@every 5m",
		}},
	})
	for _, want := range []string{
		"Plugins:",
		"cartdemo/reference",
		"Infrastructure ports:",
		"Plugin pricing steps:",
		"after:promotions",
		"Sync jobs:",
		"integration.sync.warehousedemo.warehouse.stock",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text missing %q:\n%s", want, text)
		}
	}
}

func TestFormatJSON_RoundTrip(t *testing.T) {
	report := pluginreport.Report{
		Plugins: []pluginreport.PluginStatus{{Name: "x", State: "active"}},
	}
	data, err := pluginreport.FormatJSON(report)
	if err != nil {
		t.Fatalf("FormatJSON: %v", err)
	}
	if !strings.Contains(string(data), `"name": "x"`) {
		t.Fatalf("json = %s", data)
	}
}

func TestBuild_SyncJobTriggerDetail(t *testing.T) {
	app := &plugin.App{Bootstrap: &plugin.Bootstrap{}}
	if err := app.Integration("acme").RegisterSyncJob(extapi.SyncJob{
		Name:    "pull",
		Trigger: extapi.OnEvent("order.created"),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	}); err != nil {
		t.Fatalf("RegisterSyncJob: %v", err)
	}
	report := pluginreport.Build(nil, app, &config.Config{})
	if len(report.SyncJobs) != 1 || report.SyncJobs[0].Detail != "order.created" {
		t.Fatalf("sync jobs = %+v", report.SyncJobs)
	}
}
