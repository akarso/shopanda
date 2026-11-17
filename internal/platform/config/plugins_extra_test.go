package config

import (
	"testing"
)

func TestLoad_PluginsExtraUnknownSlug(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, `
plugins:
  example:
    enabled: false
  myproduct:
    enabled: true
    api_key: "secret-value"
    fee_minor_units: 42
`)
	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	extra := cfg.Plugins.Extra
	if extra == nil || extra["myproduct"] == nil {
		t.Fatalf("Extra = %#v", extra)
	}
	if extra["myproduct"]["enabled"] != "true" {
		t.Fatalf("enabled = %q", extra["myproduct"]["enabled"])
	}
	if extra["myproduct"]["fee_minor_units"] != "42" {
		t.Fatalf("fee = %q", extra["myproduct"]["fee_minor_units"])
	}
	if extra["myproduct"]["api_key"] != "secret-value" {
		t.Fatalf("api_key = %q", extra["myproduct"]["api_key"])
	}
	if _, ok := extra["example"]; ok {
		t.Fatal("typed example must not appear in Extra")
	}

	flat := Flatten(cfg)
	if flat["plugins.myproduct.enabled"] != "true" {
		t.Fatalf("flatten enabled = %q", flat["plugins.myproduct.enabled"])
	}
	if flat["plugins.myproduct.fee_minor_units"] != "42" {
		t.Fatalf("flatten fee = %q", flat["plugins.myproduct.fee_minor_units"])
	}
	if flat["plugins.myproduct.api_key"] != "secret-value" {
		t.Fatalf("flatten api_key (unredacted Flatten) = %q", flat["plugins.myproduct.api_key"])
	}
}

func TestLoad_PluginsExtraEnvOverlay(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, `
plugins:
  sitekit:
    enabled: false
`)
	t.Setenv("SHOPANDA_PLUGINS_SITEKIT_ENABLED", "true")
	t.Setenv("SHOPANDA_PLUGINS_SITEKIT_WIDGET_ID", "w-1")
	t.Setenv("SHOPANDA_PLUGINS_EXAMPLE_ENABLED", "true")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Plugins.Extra["sitekit"]["enabled"] != "true" {
		t.Fatalf("Extra sitekit = %#v", cfg.Plugins.Extra["sitekit"])
	}
	if cfg.Plugins.Extra["sitekit"]["widget_id"] != "w-1" {
		t.Fatalf("widget_id = %#v", cfg.Plugins.Extra["sitekit"])
	}
	if _, ok := cfg.Plugins.Extra["example"]; ok {
		t.Fatal("typed example env must not create Extra")
	}
	if !cfg.Plugins.Example.Enabled {
		t.Fatal("typed example enabled should still apply from env")
	}
}
