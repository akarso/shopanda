package extapi

// ConfigFieldType describes a simple admin-editable plugin setting.
type ConfigFieldType string

const (
	ConfigFieldString ConfigFieldType = "string"
	ConfigFieldInt    ConfigFieldType = "int"
	ConfigFieldBool   ConfigFieldType = "bool"
)

// ConfigField defines one plugin setting surfaced in admin integrations.
// Get/Apply close over the plugin's own state (not core config) — a plugin
// typically seeds an initial value from Host.ConfigValue at Init and stores
// the live value on its own struct (e.g. behind a pointer or atomic field)
// so pricing/other steps observe admin updates without a restart.
type ConfigField struct {
	Key         string
	Label       string
	Description string
	Type        ConfigFieldType
	Secret      bool
	Get         func() interface{}
	Apply       func(value interface{}) error
}

// ConfigDefinition groups plugin-registered settings for admin display.
// Register via Host.RegisterConfig.
type ConfigDefinition struct {
	Plugin string
	Fields []ConfigField
}
