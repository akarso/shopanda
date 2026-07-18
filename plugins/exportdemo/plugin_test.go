package exportdemo_test

import (
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/plugins/exportdemo"
)

func testApp(cfg *config.Config) *plugin.App {
	return &plugin.App{Logger: logger.NewWithWriter(nil, "error"), Config: cfg}
}

func TestPlugin_Name(t *testing.T) {
	if got := exportdemo.New().Name(); got != "exportdemo/reference" {
		t.Fatalf("Name() = %q", got)
	}
}

func TestPlugin_Init_Disabled(t *testing.T) {
	cfg := &config.Config{Plugins: config.PluginsConfig{
		ExportDemo: config.ExportDemoPluginConfig{Enabled: false},
	}}
	if err := exportdemo.New().Init(testApp(cfg)); err == nil {
		t.Fatal("expected error when disabled")
	}
}

func TestRemapProductRow(t *testing.T) {
	ctx := &extapi.ExportRowContext{
		Row: map[string]string{
			"sku":         "SKU-001",
			"name":        "Widget",
			"description": "Desc",
			"slug":        "widget",
		},
	}
	exportdemo.RemapProductRow(ctx)
	if ctx.Row["matnr"] != "SKU-001" || ctx.Row["sku"] != "" {
		t.Fatalf("row = %v", ctx.Row)
	}
	if !strings.Contains(ctx.Row["maktx"], "Widget") {
		t.Fatalf("row = %v", ctx.Row)
	}
}
