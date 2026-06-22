package example

import (
	"fmt"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

const configKeyFeeMinorUnits = "plugins.example.fee_minor_units"

func examplePluginConfigDefinition() plugin.ConfigDefinition {
	return plugin.ConfigDefinition{
		Plugin: "example/demo",
		Fields: []plugin.ConfigField{
			{
				Key:         configKeyFeeMinorUnits,
				Label:       "Example fee (minor units)",
				Description: "Fixed fee added by the example pricing step (e.g. 100 = 1.00 for two-decimal currencies).",
				Type:        plugin.ConfigFieldInt,
				Get: func(cfg *config.Config) interface{} {
					if cfg == nil {
						return int64(defaultExampleFeeMinorUnits)
					}
					if cfg.Plugins.Example.FeeMinorUnits > 0 {
						return cfg.Plugins.Example.FeeMinorUnits
					}
					return int64(defaultExampleFeeMinorUnits)
				},
				Apply: func(cfg *config.Config, value interface{}) error {
					if cfg == nil {
						return fmt.Errorf("example plugin: config must not be nil")
					}
					n, err := toInt64(value)
					if err != nil {
						return err
					}
					if n < 0 {
						return fmt.Errorf("example plugin: fee_minor_units must be non-negative")
					}
					cfg.Plugins.Example.FeeMinorUnits = n
					return nil
				},
			},
		},
	}
}

func toInt64(value interface{}) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case int:
		return int64(typed), nil
	case float64:
		return int64(typed), nil
	default:
		return 0, fmt.Errorf("unsupported int value type %T", value)
	}
}
