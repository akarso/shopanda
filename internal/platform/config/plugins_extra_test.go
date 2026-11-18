package config

import (
	"strings"
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

func TestLoad_PluginsExtraNestedMap(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, `
plugins:
  myproduct:
    enabled: true
    generation:
      provider: openai
      model: "gpt-4"
`)
	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	extra := cfg.Plugins.Extra["myproduct"]
	if extra["generation.provider"] != "openai" {
		t.Fatalf("dotted = %#v", extra)
	}
	if extra["generation_provider"] != "openai" {
		t.Fatalf("underscore alias = %#v", extra)
	}
	if extra["generation.model"] != "gpt-4" {
		t.Fatalf("model = %#v", extra)
	}
	flat := Flatten(cfg)
	if flat["plugins.myproduct.generation.provider"] != "openai" {
		t.Fatalf("flatten dotted = %q", flat["plugins.myproduct.generation.provider"])
	}
	if flat["plugins.myproduct.generation_provider"] != "openai" {
		t.Fatalf("flatten underscore = %q", flat["plugins.myproduct.generation_provider"])
	}
}

func TestLoad_PluginsExtraNestedEnvOverridesYAML(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, `
plugins:
  myproduct:
    generation:
      provider: yaml-provider
`)
	// Env form uses underscores; must override YAML dotted key.
	t.Setenv("SHOPANDA_PLUGINS_MYPRODUCT_GENERATION_PROVIDER", "env-provider")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	extra := cfg.Plugins.Extra["myproduct"]
	if extra["generation.provider"] != "env-provider" {
		t.Fatalf("dotted after env = %#v", extra)
	}
	if extra["generation_provider"] != "env-provider" {
		t.Fatalf("underscore after env = %#v", extra)
	}
	flat := Flatten(cfg)
	if flat["plugins.myproduct.generation.provider"] != "env-provider" {
		t.Fatalf("flatten = %q", flat["plugins.myproduct.generation.provider"])
	}
}

func TestLoad_PluginsExtraListRejected(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, `
plugins:
  myproduct:
    tags:
      - a
      - b
`)
	_, err := loadCfg(t, path)
	if err == nil || !strings.Contains(err.Error(), "lists are not supported") {
		t.Fatalf("err = %v, want lists rejected", err)
	}
}
