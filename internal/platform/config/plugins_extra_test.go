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

func TestLoad_PluginsExtraSecretAliasRedacted(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, `
plugins:
  myproduct:
    api_key: "secret-value"
    warehouse_api_key: "wh-secret"
`)
	_, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Package Get uses the redacted flatten from Load.
	if got := Get("plugins.myproduct.api_key"); got != "***" {
		t.Fatalf("api_key Get = %q, want ***", got)
	}
	if got := Get("plugins.myproduct.api.key"); got != "***" {
		t.Fatalf("api.key alias Get = %q, want *** (must not leak)", got)
	}
	if got := Get("plugins.myproduct.warehouse_api_key"); got != "***" {
		t.Fatalf("warehouse_api_key = %q", got)
	}
	if got := Get("plugins.myproduct.warehouse.api.key"); got != "***" {
		t.Fatalf("warehouse.api.key alias = %q, want ***", got)
	}
	// Unredacted Flatten still exposes secrets for Host.Config*.
	cfg, _ := loadCfg(t, path)
	flat := Flatten(cfg)
	if flat["plugins.myproduct.api.key"] != "secret-value" {
		t.Fatalf("unredacted Flatten alias = %q", flat["plugins.myproduct.api.key"])
	}
}

func TestLoad_PluginsExtraConflictingFlatAndNested(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, `
plugins:
  myproduct:
    my_key: a
    my:
      key: b
`)
	_, err := loadCfg(t, path)
	if err == nil || !strings.Contains(err.Error(), "conflicting keys") {
		t.Fatalf("err = %v, want conflicting keys", err)
	}
}

func TestLoad_PluginsExtraEnvAliasConflict(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, `
plugins:
  myproduct:
    enabled: true
`)
	// Underscore and dotted env forms alias to the same Extra keys.
	t.Setenv("SHOPANDA_PLUGINS_MYPRODUCT_MY_KEY", "a")
	t.Setenv("SHOPANDA_PLUGINS_MYPRODUCT_MY.KEY", "b")
	_, err := loadCfg(t, path)
	if err == nil || !strings.Contains(err.Error(), "conflicting keys") {
		t.Fatalf("err = %v, want env conflicting keys", err)
	}
}

func TestSetPluginExtraKey_ConflictBothOrders(t *testing.T) {
	m := map[string]string{}
	if err := setPluginExtraKey(m, "my_key", "a"); err != nil {
		t.Fatal(err)
	}
	if err := setPluginExtraKey(m, "my.key", "b"); err == nil {
		t.Fatal("expected conflict underscore-then-dot")
	}
	m2 := map[string]string{}
	if err := setPluginExtraKey(m2, "my.key", "b"); err != nil {
		t.Fatal(err)
	}
	if err := setPluginExtraKey(m2, "my_key", "a"); err == nil {
		t.Fatal("expected conflict dot-then-underscore")
	}
}

func TestLoad_PluginsExtraThreeLevelNesting(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, `
plugins:
  myproduct:
    generation:
      openai:
        model: "gpt-4"
`)
	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Plugins.Extra["myproduct"]["generation.openai.model"] != "gpt-4" {
		t.Fatalf("extra = %#v", cfg.Plugins.Extra["myproduct"])
	}
	if cfg.Plugins.Extra["myproduct"]["generation_openai_model"] != "gpt-4" {
		t.Fatalf("underscore alias missing: %#v", cfg.Plugins.Extra["myproduct"])
	}
}
