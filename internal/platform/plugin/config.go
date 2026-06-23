package plugin

import (
	"context"
	"fmt"
	"math"
	"sort"

	domainCfg "github.com/akarso/shopanda/internal/domain/config"
	"github.com/akarso/shopanda/internal/platform/config"
)

// ConfigFieldType describes a simple admin-editable plugin setting.
type ConfigFieldType string

const (
	ConfigFieldString ConfigFieldType = "string"
	ConfigFieldInt    ConfigFieldType = "int"
	ConfigFieldBool   ConfigFieldType = "bool"
)

// ConfigField defines one plugin setting surfaced in admin integrations.
type ConfigField struct {
	Key         string
	Label       string
	Description string
	Type        ConfigFieldType
	Secret      bool
	Get         func(cfg *config.Config) interface{}
	Apply       func(cfg *config.Config, value interface{}) error
}

// ConfigDefinition groups plugin-registered settings for admin display.
type ConfigDefinition struct {
	Plugin string
	Fields []ConfigField
}

// ConfigRegistry collects plugin config schemas registered during Init.
type ConfigRegistry struct {
	fields map[string]ConfigField
	plugins map[string]string // key -> plugin name
}

// NewConfigRegistry creates an empty plugin config registry.
func NewConfigRegistry() *ConfigRegistry {
	return &ConfigRegistry{
		fields:  make(map[string]ConfigField),
		plugins: make(map[string]string),
	}
}

// Register adds fields from a plugin config definition.
func (r *ConfigRegistry) Register(def ConfigDefinition) error {
	if r == nil {
		return fmt.Errorf("plugin: config registry must not be nil")
	}
	if def.Plugin == "" {
		return fmt.Errorf("plugin: config definition plugin name must not be empty")
	}
	for _, field := range def.Fields {
		if field.Key == "" {
			return fmt.Errorf("plugin: config field key must not be empty")
		}
		if field.Label == "" {
			return fmt.Errorf("plugin: config field label must not be empty for key %q", field.Key)
		}
		if field.Type == "" {
			return fmt.Errorf("plugin: config field type must not be empty for key %q", field.Key)
		}
		if field.Apply == nil {
			return fmt.Errorf("plugin: config field apply func must not be nil for key %q", field.Key)
		}
		if field.Get == nil {
			return fmt.Errorf("plugin: config field get func must not be nil for key %q", field.Key)
		}
		if _, exists := r.fields[field.Key]; exists {
			return fmt.Errorf("plugin: duplicate config key %q", field.Key)
		}
		r.fields[field.Key] = field
		r.plugins[field.Key] = def.Plugin
	}
	return nil
}

// Keys returns registered config keys in stable order.
func (r *ConfigRegistry) Keys() []string {
	if r == nil || len(r.fields) == 0 {
		return nil
	}
	keys := make([]string, 0, len(r.fields))
	for key := range r.fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// HasKey reports whether key was registered by a plugin.
func (r *ConfigRegistry) HasKey(key string) bool {
	if r == nil {
		return false
	}
	_, ok := r.fields[key]
	return ok
}

// Field returns metadata for a registered key.
func (r *ConfigRegistry) Field(key string) (ConfigField, bool) {
	if r == nil {
		return ConfigField{}, false
	}
	field, ok := r.fields[key]
	return field, ok
}

// PluginName returns the plugin that registered key.
func (r *ConfigRegistry) PluginName(key string) string {
	if r == nil {
		return ""
	}
	return r.plugins[key]
}

// Definitions returns grouped field metadata for admin APIs.
func (r *ConfigRegistry) Definitions() []map[string]interface{} {
	if r == nil || len(r.fields) == 0 {
		return nil
	}
	keys := r.Keys()
	out := make([]map[string]interface{}, 0, len(keys))
	for _, key := range keys {
		field := r.fields[key]
		out = append(out, map[string]interface{}{
			"key":         field.Key,
			"label":       field.Label,
			"description": field.Description,
			"type":        string(field.Type),
			"secret":      field.Secret,
			"plugin":      r.plugins[key],
		})
	}
	return out
}

// ValueFromConfig reads the current in-memory value for key from cfg.
func (r *ConfigRegistry) ValueFromConfig(cfg *config.Config, key string) interface{} {
	field, ok := r.Field(key)
	if !ok || cfg == nil || field.Get == nil {
		return nil
	}
	return field.Get(cfg)
}

// CoerceValue validates and normalizes a plugin config value to its declared type.
func (r *ConfigRegistry) CoerceValue(key string, value interface{}) (interface{}, error) {
	field, ok := r.Field(key)
	if !ok {
		return nil, fmt.Errorf("plugin: unknown config key %q", key)
	}
	return normalizeConfigValue(field, value)
}

// ApplyToConfig updates in-memory cfg from persisted entries.
func (r *ConfigRegistry) ApplyToConfig(cfg *config.Config, entries map[string]interface{}) error {
	if r == nil || cfg == nil {
		return nil
	}
	for key, value := range entries {
		if err := r.applyOne(cfg, key, value); err != nil {
			return err
		}
	}
	return nil
}

// SnapshotValues captures current in-memory values for registered plugin keys in entries.
func (r *ConfigRegistry) SnapshotValues(cfg *config.Config, entries map[string]interface{}) map[string]interface{} {
	if r == nil || cfg == nil || len(entries) == 0 {
		return nil
	}
	out := make(map[string]interface{})
	for key := range entries {
		if !r.HasKey(key) {
			continue
		}
		out[key] = r.ValueFromConfig(cfg, key)
	}
	return out
}

// LoadPersisted overlays DB-stored values onto cfg for registered keys.
func LoadPersisted(ctx context.Context, repo domainCfg.Repository, cfg *config.Config, registry *ConfigRegistry) error {
	if repo == nil || cfg == nil || registry == nil {
		return nil
	}
	for _, key := range registry.Keys() {
		value, err := repo.Get(ctx, key)
		if err != nil {
			return fmt.Errorf("plugin config load %q: %w", key, err)
		}
		if value == nil {
			continue
		}
		if err := registry.applyOne(cfg, key, value); err != nil {
			return fmt.Errorf("plugin config apply %q: %w", key, err)
		}
	}
	return nil
}

func (r *ConfigRegistry) applyOne(cfg *config.Config, key string, value interface{}) error {
	field, ok := r.Field(key)
	if !ok {
		return fmt.Errorf("plugin: unknown config key %q", key)
	}
	normalized, err := normalizeConfigValue(field, value)
	if err != nil {
		return err
	}
	return field.Apply(cfg, normalized)
}

func normalizeConfigValue(field ConfigField, value interface{}) (interface{}, error) {
	switch field.Type {
	case ConfigFieldString:
		switch typed := value.(type) {
		case string:
			return typed, nil
		case fmt.Stringer:
			return typed.String(), nil
		default:
			return fmt.Sprintf("%v", typed), nil
		}
	case ConfigFieldBool:
		switch typed := value.(type) {
		case bool:
			return typed, nil
		case float64:
			return typed != 0, nil
		case int:
			return typed != 0, nil
		case int64:
			return typed != 0, nil
		default:
			return false, fmt.Errorf("plugin: config value must be bool for key %q", field.Key)
		}
	case ConfigFieldInt:
		n, err := toInt64(value)
		if err != nil {
			return nil, fmt.Errorf("plugin: config value must be int for key %q: %w", field.Key, err)
		}
		return n, nil
	default:
		return nil, fmt.Errorf("plugin: unsupported config field type %q", field.Type)
	}
}

func toInt64(value interface{}) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case int:
		return int64(typed), nil
	case float64:
		if typed != math.Trunc(typed) {
			return 0, fmt.Errorf("non-integer float64 %v", typed)
		}
		if typed < float64(math.MinInt64) || typed > float64(math.MaxInt64) {
			return 0, fmt.Errorf("float64 %v out of int64 range", typed)
		}
		return int64(typed), nil
	case jsonNumber:
		return typed.Int64()
	default:
		return 0, fmt.Errorf("unsupported type %T", value)
	}
}

// jsonNumber matches encoding/json.Number without importing encoding/json in hot path tests.
type jsonNumber interface {
	Int64() (int64, error)
}
