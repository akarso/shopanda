package example

import (
	"fmt"

	"github.com/akarso/shopanda/pkg/extapi"
)

const configKeyFeeMinorUnits = "plugins.example.fee_minor_units"

// exampleFeeConfigDefinition builds the admin-editable config definition for
// feeMinorUnits. Get/Apply close over feeMinorUnits directly — the plugin
// owns its own live value, rather than reaching back into core config.
func exampleFeeConfigDefinition(feeMinorUnits *int64) extapi.ConfigDefinition {
	return extapi.ConfigDefinition{
		Plugin: "example/demo",
		Fields: []extapi.ConfigField{
			{
				Key:         configKeyFeeMinorUnits,
				Label:       "Example fee (minor units)",
				Description: "Fixed fee added by the example pricing step (e.g. 100 = 1.00 for two-decimal currencies).",
				Type:        extapi.ConfigFieldInt,
				Get: func() interface{} {
					if feeMinorUnits != nil && *feeMinorUnits > 0 {
						return *feeMinorUnits
					}
					return int64(defaultExampleFeeMinorUnits)
				},
				Apply: func(value interface{}) error {
					n, err := toInt64(value)
					if err != nil {
						return err
					}
					if n <= 0 {
						return fmt.Errorf("example plugin: fee_minor_units must be positive")
					}
					*feeMinorUnits = n
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
