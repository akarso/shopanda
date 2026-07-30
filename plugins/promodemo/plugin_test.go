package promodemo_test

import (
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/domain/promotion"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/promodemo"
)

func testApp(cfg *config.Config) *plugin.App {
	app := &plugin.App{
		Logger: logger.NewWithWriter(io.Discard, "error"),
		Config: cfg,
	}
	app.SetPromotionEvaluatorRegistry(promotion.NewEvaluatorRegistry())
	return app
}

func TestPlugin_Name(t *testing.T) {
	if got := promodemo.New().Name(); got != "promodemo/reference" {
		t.Fatalf("Name() = %q", got)
	}
}

func TestPlugin_Init_Disabled(t *testing.T) {
	cfg := &config.Config{Plugins: config.PluginsConfig{
		PromoDemo: config.PromoDemoPluginConfig{Enabled: false},
	}}
	if err := promodemo.New().Init(testApp(cfg)); err == nil {
		t.Fatal("expected error when disabled")
	}
}

func TestPlugin_Init_RegistersRuleTypes(t *testing.T) {
	cfg := &config.Config{Plugins: config.PluginsConfig{
		PromoDemo: config.PromoDemoPluginConfig{Enabled: true},
	}}
	app := testApp(cfg)
	if err := promodemo.New().Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	reg := app.PromotionEvaluatorRegistry()
	if reg == nil {
		t.Fatal("registry is nil")
	}
	if !reg.HasCatalogCondition(promodemo.RuleMinLineTotal) {
		t.Fatal("expected min_line_total condition")
	}
	if !reg.HasCatalogAction(promodemo.RuleLineBonusPercent) {
		t.Fatal("expected line_bonus_percent action")
	}
}
