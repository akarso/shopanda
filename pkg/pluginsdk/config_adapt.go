package pluginsdk

import (
	"fmt"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
)

// adaptConfigDefinition wraps a public ConfigDefinition (Get/Apply closures
// over the plugin's own state) as a core plugin.ConfigDefinition (Get/Apply
// closures over *config.Config, unused here by design — public plugins never
// read or write core config directly).
func adaptConfigDefinition(def extapi.ConfigDefinition) (plugin.ConfigDefinition, error) {
	if def.Plugin == "" {
		return plugin.ConfigDefinition{}, fmt.Errorf("pluginsdk: RegisterConfig: plugin name must not be empty")
	}
	fields := make([]plugin.ConfigField, 0, len(def.Fields))
	for _, f := range def.Fields {
		if f.Get == nil || f.Apply == nil {
			return plugin.ConfigDefinition{}, fmt.Errorf("pluginsdk: RegisterConfig: field %q must set Get and Apply", f.Key)
		}
		get := f.Get
		apply := f.Apply
		fields = append(fields, plugin.ConfigField{
			Key:         f.Key,
			Label:       f.Label,
			Description: f.Description,
			Type:        plugin.ConfigFieldType(f.Type),
			Secret:      f.Secret,
			Get: func(_ *config.Config) interface{} {
				return get()
			},
			Apply: func(_ *config.Config, value interface{}) error {
				return apply(value)
			},
		})
	}
	return plugin.ConfigDefinition{Plugin: def.Plugin, Fields: fields}, nil
}
