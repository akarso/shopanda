package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// typedPluginYAMLKeys are PluginsConfig fields with dedicated structs. Unknown
// YAML sibling keys under plugins: land in Extra (product plugins). Typed
// wins when both exist — Extra never shadows these keys.
var typedPluginYAMLKeys = map[string]struct{}{
	"depends_on":       {},
	"core":             {},
	"graphql":          {},
	"example":          {},
	"slotsdemo":        {},
	"cartdemo":         {},
	"taxdemo":          {},
	"maildemo":         {},
	"promodemo":        {},
	"importdemo":       {},
	"exportdemo":       {},
	"checkoutdemo":     {},
	"integrationdemo":  {},
	"warehousedemo":    {},
	"pimdemo":          {},
	"b2b":              {},
	"extref":           {},
	"metaldogs":        {}, // migrate-next: prefer Extra + Host.Config* for product forks
}

// UnmarshalYAML decodes typed plugin fields and captures unknown plugin slugs
// into Extra as stringly maps (Host.Config* / Flatten consume them).
func (p *PluginsConfig) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		return nil
	}
	var raw map[string]yaml.Node
	if err := value.Decode(&raw); err != nil {
		return err
	}

	type plain PluginsConfig
	var decoded plain
	if err := value.Decode(&decoded); err != nil {
		return err
	}
	*p = PluginsConfig(decoded)
	p.Extra = nil

	for key, node := range raw {
		slug := strings.TrimSpace(strings.ToLower(key))
		if slug == "" {
			continue
		}
		if _, typed := typedPluginYAMLKeys[slug]; typed {
			continue
		}
		flat, err := yamlMapToStringMap(&node)
		if err != nil {
			return fmt.Errorf("plugins.%s: %w", slug, err)
		}
		if len(flat) == 0 {
			continue
		}
		if p.Extra == nil {
			p.Extra = make(map[string]map[string]string)
		}
		p.Extra[slug] = flat
	}
	return nil
}

func yamlMapToStringMap(node *yaml.Node) (map[string]string, error) {
	if node == nil || node.Kind == yaml.ScalarNode && strings.TrimSpace(node.Value) == "" {
		return nil, nil
	}
	var m map[string]interface{}
	if err := node.Decode(&m); err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		key := strings.TrimSpace(strings.ToLower(k))
		if key == "" {
			continue
		}
		out[key] = stringifyYAMLScalar(v)
	}
	return out, nil
}

func stringifyYAMLScalar(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case int:
		return fmt.Sprintf("%d", t)
	case int64:
		return fmt.Sprintf("%d", t)
	case uint64:
		return fmt.Sprintf("%d", t)
	case float64:
		// yaml.v3 decodes integers as int; floats stay float64
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func flattenPluginsExtra(m map[string]string, cfg *Config, redactFn func(string) string) {
	if cfg == nil || len(cfg.Plugins.Extra) == 0 {
		return
	}
	if redactFn == nil {
		redactFn = func(s string) string { return s }
	}
	for slug, kv := range cfg.Plugins.Extra {
		if _, typed := typedPluginYAMLKeys[slug]; typed {
			continue
		}
		for key, val := range kv {
			flatKey := "plugins." + slug + "." + key
			if looksSecretPluginKey(key) {
				m[flatKey] = redactFn(val)
			} else {
				m[flatKey] = val
			}
		}
	}
}

func looksSecretPluginKey(key string) bool {
	k := strings.ToLower(key)
	return strings.Contains(k, "secret") ||
		strings.Contains(k, "api_key") ||
		strings.Contains(k, "license") ||
		strings.HasSuffix(k, "_key") && strings.Contains(k, "license")
}

// applyPluginsExtraEnv overlays SHOPANDA_PLUGINS_<SLUG>_<KEY> onto Extra for
// slugs that are not typed fields. Slug is the first underscore-separated
// token after the prefix (lowercased); the remainder is the key (lowercased).
// Product slugs with underscores in the slug itself should set values via YAML.
func applyPluginsExtraEnv(cfg *Config) {
	if cfg == nil {
		return
	}
	const prefix = "SHOPANDA_PLUGINS_"
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, prefix) {
			continue
		}
		kv := strings.SplitN(e, "=", 2)
		if len(kv) != 2 {
			continue
		}
		rest := strings.TrimPrefix(kv[0], prefix)
		if rest == "" {
			continue
		}
		if isTypedPluginEnvRest(rest) {
			continue
		}
		idx := strings.IndexByte(rest, '_')
		if idx <= 0 || idx == len(rest)-1 {
			continue
		}
		slug := strings.ToLower(rest[:idx])
		key := strings.ToLower(rest[idx+1:])
		if slug == "" || key == "" {
			continue
		}
		if _, typed := typedPluginYAMLKeys[slug]; typed {
			continue
		}
		if cfg.Plugins.Extra == nil {
			cfg.Plugins.Extra = make(map[string]map[string]string)
		}
		if cfg.Plugins.Extra[slug] == nil {
			cfg.Plugins.Extra[slug] = make(map[string]string)
		}
		cfg.Plugins.Extra[slug][key] = kv[1]
	}
}

func isTypedPluginEnvRest(rest string) bool {
	upper := strings.ToUpper(rest)
	for slug := range typedPluginYAMLKeys {
		if slug == "depends_on" || slug == "core" {
			continue
		}
		u := strings.ToUpper(slug)
		if upper == u || strings.HasPrefix(upper, u+"_") {
			return true
		}
	}
	return false
}
