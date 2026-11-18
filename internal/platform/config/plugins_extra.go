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
	out := make(map[string]string)
	if err := flattenYAMLValue("", m, out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	// Keep . / _ aliases so env SHOPANDA_PLUGINS_<SLUG>_GENERATION_PROVIDER
	// matches YAML generation.provider through Host.Config* / Flatten.
	// Both forms are written (exact lookup); conflicting values fail closed.
	synced := make(map[string]string, len(out)*2)
	for k, v := range out {
		if err := setPluginExtraKey(synced, k, v); err != nil {
			return nil, err
		}
	}
	return synced, nil
}

// flattenYAMLValue writes dotted keys into out. Nested maps become
// "generation.provider"; lists are rejected (Host.Config* is stringly).
func flattenYAMLValue(prefix string, v interface{}, out map[string]string) error {
	switch t := v.(type) {
	case map[string]interface{}:
		if len(t) == 0 {
			return nil
		}
		for k, child := range t {
			key := strings.TrimSpace(strings.ToLower(k))
			if key == "" {
				continue
			}
			full := key
			if prefix != "" {
				full = prefix + "." + key
			}
			if err := flattenYAMLValue(full, child, out); err != nil {
				return err
			}
		}
		return nil
	case map[interface{}]interface{}:
		// Defensive: some YAML paths decode map keys as interface{}.
		norm := make(map[string]interface{}, len(t))
		for k, child := range t {
			norm[fmt.Sprint(k)] = child
		}
		return flattenYAMLValue(prefix, norm, out)
	case []interface{}:
		path := prefix
		if path == "" {
			path = "(root)"
		}
		return fmt.Errorf("lists are not supported at %q (use a scalar or nested map)", path)
	default:
		if prefix == "" {
			return fmt.Errorf("plugin config must be a map")
		}
		out[prefix] = stringifyYAMLScalar(v)
		return nil
	}
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

// setPluginExtraKey writes key=val and keeps '.' / '_' aliases in sync so
// YAML generation.provider and env GENERATION_PROVIDER both resolve via
// Host.Config* / Flatten (exact map keys — both forms are stored). Returns
// an error when an alias already exists with a different value (YAML
// collision). Env overlay uses overwritePluginExtraKey instead.
func setPluginExtraKey(m map[string]string, key, val string) error {
	return writePluginExtraKey(m, key, val, false)
}

func overwritePluginExtraKey(m map[string]string, key, val string) {
	_ = writePluginExtraKey(m, key, val, true)
}

func writePluginExtraKey(m map[string]string, key, val string, overwrite bool) error {
	if m == nil || key == "" {
		return nil
	}
	forms := []string{key}
	if alt := strings.ReplaceAll(key, "_", "."); alt != key {
		forms = append(forms, alt)
	}
	if alt := strings.ReplaceAll(key, ".", "_"); alt != key {
		forms = append(forms, alt)
	}
	if !overwrite {
		for _, k := range forms {
			if existing, ok := m[k]; ok && existing != val {
				return fmt.Errorf("conflicting keys %q and %q ('.' and '_' are aliases; values %q vs %q)",
					key, k, val, existing)
			}
		}
	}
	for _, k := range forms {
		m[k] = val
	}
	return nil
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
	// Normalise dotted aliases (api.key) so they match underscore forms (api_key).
	u := strings.ReplaceAll(k, ".", "_")
	return strings.Contains(u, "secret") ||
		strings.Contains(u, "api_key") ||
		strings.Contains(u, "license") ||
		strings.HasSuffix(u, "_key") && strings.Contains(u, "license")
}

// applyPluginsExtraEnv overlays SHOPANDA_PLUGINS_<SLUG>_<KEY> onto Extra for
// slugs that are not typed fields. Slug is the first underscore-separated
// token after the prefix (lowercased); the remainder is the key (lowercased).
// Product slugs with underscores in the slug itself should set values via YAML.
// Env intentionally overrides YAML. Conflicting env keys that alias to the
// same path ('.' ≡ '_') fail closed — same rule as YAML.
func applyPluginsExtraEnv(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	const prefix = "SHOPANDA_PLUGINS_"
	// Collect first so env↔env alias conflicts are detected before YAML merge.
	pending := map[string]map[string]string{}
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
		if pending[slug] == nil {
			pending[slug] = make(map[string]string)
		}
		if err := setPluginExtraKey(pending[slug], key, kv[1]); err != nil {
			return fmt.Errorf("plugins.%s: env: %w", slug, err)
		}
	}
	for slug, kv := range pending {
		if cfg.Plugins.Extra == nil {
			cfg.Plugins.Extra = make(map[string]map[string]string)
		}
		if cfg.Plugins.Extra[slug] == nil {
			cfg.Plugins.Extra[slug] = make(map[string]string)
		}
		for k, v := range kv {
			cfg.Plugins.Extra[slug][k] = v
		}
	}
	return nil
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
