package config

// PlatformConfigAllowlist is the fixed set of non-secret core settings a
// pluginsdk.Host may read via PlatformConfig. Keep in sync with driver
// switches in ports.Catalog ConfigKey (search/cache/queue/media). Secrets
// (database.*, queue.*.url, webhooks.*, tracing headers, plugins.*) stay out.
var PlatformConfigAllowlist = map[string]struct{}{
	"search.engine":  {},
	"cache.driver":   {},
	"queue.driver":   {},
	"media.storage":  {},
}

// IsPlatformConfigKey reports whether key is allowlisted for Host.PlatformConfig.
func IsPlatformConfigKey(key string) bool {
	_, ok := PlatformConfigAllowlist[key]
	return ok
}
