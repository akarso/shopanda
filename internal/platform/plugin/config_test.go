package plugin_test

import (
	"context"
	"testing"

	domainCfg "github.com/akarso/shopanda/internal/domain/config"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

type memConfigRepo struct {
	values map[string]interface{}
}

func (m *memConfigRepo) Get(_ context.Context, key string) (interface{}, error) {
	return m.values[key], nil
}

func (m *memConfigRepo) Set(_ context.Context, key string, value interface{}) error {
	m.values[key] = value
	return nil
}

func (m *memConfigRepo) SetMany(_ context.Context, entries map[string]interface{}) error {
	for k, v := range entries {
		m.values[k] = v
	}
	return nil
}

func (m *memConfigRepo) Delete(_ context.Context, key string) error {
	delete(m.values, key)
	return nil
}

func (m *memConfigRepo) All(_ context.Context) ([]domainCfg.Entry, error) {
	return nil, nil
}

func TestConfigRegistry_RegisterAndApply(t *testing.T) {
	reg := plugin.NewConfigRegistry()
	cfg := &config.Config{}
	err := reg.Register(plugin.ConfigDefinition{
		Plugin: "test/plugin",
		Fields: []plugin.ConfigField{
			{
				Key:   "plugins.test.value",
				Label: "Test value",
				Type:  plugin.ConfigFieldInt,
				Get: func(c *config.Config) interface{} {
					return c.Plugins.Example.FeeMinorUnits
				},
				Apply: func(c *config.Config, value interface{}) error {
					c.Plugins.Example.FeeMinorUnits = value.(int64)
					return nil
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	if err := reg.ApplyToConfig(cfg, map[string]interface{}{"plugins.test.value": float64(250)}); err != nil {
		t.Fatalf("ApplyToConfig() error: %v", err)
	}
	if cfg.Plugins.Example.FeeMinorUnits != 250 {
		t.Fatalf("FeeMinorUnits = %d, want 250", cfg.Plugins.Example.FeeMinorUnits)
	}
}

func TestLoadPersisted_OverlaysRegisteredKeys(t *testing.T) {
	reg := plugin.NewConfigRegistry()
	cfg := &config.Config{}
	if err := reg.Register(plugin.ConfigDefinition{
		Plugin: "test/plugin",
		Fields: []plugin.ConfigField{
			{
				Key:   "plugins.test.value",
				Label: "Test value",
				Type:  plugin.ConfigFieldInt,
				Get: func(c *config.Config) interface{} {
					return c.Plugins.Example.FeeMinorUnits
				},
				Apply: func(c *config.Config, value interface{}) error {
					switch v := value.(type) {
					case int64:
						c.Plugins.Example.FeeMinorUnits = v
					case float64:
						c.Plugins.Example.FeeMinorUnits = int64(v)
					default:
						t.Fatalf("unexpected type %T", value)
					}
					return nil
				},
			},
		},
	}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	repo := &memConfigRepo{values: map[string]interface{}{
		"plugins.test.value": float64(175),
	}}
	if err := plugin.LoadPersisted(context.Background(), repo, cfg, reg); err != nil {
		t.Fatalf("LoadPersisted() error: %v", err)
	}
	if cfg.Plugins.Example.FeeMinorUnits != 175 {
		t.Fatalf("FeeMinorUnits = %d, want 175", cfg.Plugins.Example.FeeMinorUnits)
	}
}

func TestApp_RegisterConfig(t *testing.T) {
	app := &plugin.App{Config: &config.Config{}}
	if err := app.RegisterConfig(plugin.ConfigDefinition{
		Plugin: "test/plugin",
		Fields: []plugin.ConfigField{
			{
				Key:   "plugins.test.flag",
				Label: "Flag",
				Type:  plugin.ConfigFieldBool,
				Get:   func(*config.Config) interface{} { return false },
				Apply: func(*config.Config, interface{}) error { return nil },
			},
		},
	}); err != nil {
		t.Fatalf("RegisterConfig() error: %v", err)
	}
}

func TestToInt64_Float64OutOfRange(t *testing.T) {
	reg := plugin.NewConfigRegistry()
	if err := reg.Register(plugin.ConfigDefinition{
		Plugin: "test/plugin",
		Fields: []plugin.ConfigField{{
			Key: "plugins.test.n", Label: "N", Type: plugin.ConfigFieldInt,
			Get:   func(*config.Config) interface{} { return int64(0) },
			Apply: func(*config.Config, interface{}) error { return nil },
		}},
	}); err != nil {
		t.Fatalf("Register() error: %v", err)
	}
	if _, err := reg.CoerceValue("plugins.test.n", float64(1e20)); err == nil {
		t.Fatal("expected out-of-range float64 to be rejected")
	}
}
