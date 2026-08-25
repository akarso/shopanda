package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/platform/jwt"
	"github.com/akarso/shopanda/internal/platform/jwt/jwttest"
)

// withTestBaseURL sets a valid PublicBaseURL via env so tests that don't
// exercise PublicBaseURL logic are not affected by wildcard-host rejection.
// It also sets a valid JWT secret and enables SHOPANDA_DEV_MODE so default
// sslmode=disable on localhost passes secure-by-default checks.
func withTestBaseURL(t *testing.T) {
	t.Helper()
	t.Setenv("SHOPANDA_SERVER_PUBLIC_BASE_URL", "http://test.localhost:8080")
	t.Setenv("SHOPANDA_AUTH_JWT_SECRET", jwttest.TestSecret)
	ensureTestDevMode(t)
}

func TestLoad_Defaults(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, "")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Server.Host = %q, want %q", cfg.Server.Host, "0.0.0.0")
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port = %d, want %d", cfg.Server.Port, 8080)
	}
	if cfg.Database.Name != "shopanda" {
		t.Errorf("Database.Name = %q, want %q", cfg.Database.Name, "shopanda")
	}
	if cfg.Log.Level != "info" {
		t.Errorf("Log.Level = %q, want %q", cfg.Log.Level, "info")
	}
	if !cfg.RateLimit.Enabled {
		t.Error("RateLimit.Enabled = false, want true by default")
	}
	if cfg.RateLimit.Default.Rate != 10 || cfg.RateLimit.Default.Burst != 20 {
		t.Errorf("RateLimit.Default = {%v,%d}, want {10,20}", cfg.RateLimit.Default.Rate, cfg.RateLimit.Default.Burst)
	}
	if !cfg.Auth.Lockout.Enabled {
		t.Error("Auth.Lockout.Enabled = false, want true by default")
	}
	if cfg.Auth.Lockout.Store != "cache" {
		t.Errorf("Auth.Lockout.Store = %q, want cache", cfg.Auth.Lockout.Store)
	}
	if cfg.Auth.Lockout.MaxFailures != 10 {
		t.Errorf("Auth.Lockout.MaxFailures = %d, want 10", cfg.Auth.Lockout.MaxFailures)
	}
	if cfg.Auth.Lockout.Window != "15m" {
		t.Errorf("Auth.Lockout.Window = %q, want 15m", cfg.Auth.Lockout.Window)
	}
	if cfg.HTTP.MaxBodyBytes != DefaultHTTPMaxBodyBytes {
		t.Errorf("HTTP.MaxBodyBytes = %d, want %d", cfg.HTTP.MaxBodyBytes, DefaultHTTPMaxBodyBytes)
	}
	if cfg.HTTP.MediaMaxBodyBytes != DefaultHTTPMediaMaxBodyBytes {
		t.Errorf("HTTP.MediaMaxBodyBytes = %d, want %d", cfg.HTTP.MediaMaxBodyBytes, DefaultHTTPMediaMaxBodyBytes)
	}
	if cfg.StoreCredit.MaxIssueAmount != DefaultStoreCreditMaxIssueAmount {
		t.Errorf("StoreCredit.MaxIssueAmount = %d, want %d", cfg.StoreCredit.MaxIssueAmount, DefaultStoreCreditMaxIssueAmount)
	}
}

func TestHTTPConfig_MediaCapIndependentOfMaxBody(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, `
http:
  max_body_bytes: 52428800
  media_max_body_bytes: 10485760
`)
	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.HTTP.MaxBodyBytes != 52428800 {
		t.Errorf("MaxBodyBytes = %d, want 52428800", cfg.HTTP.MaxBodyBytes)
	}
	if cfg.HTTP.MediaMaxBodyBytes != 10485760 {
		t.Errorf("MediaMaxBodyBytes = %d, want 10485760 (must not be raised to match max_body)", cfg.HTTP.MediaMaxBodyBytes)
	}
}

func TestLoad_YAMLOverridesDefaults(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
server:
  port: 9090
log:
  level: debug
`
	path := writeYAML(t, yaml)

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Server.Port != 9090 {
		t.Errorf("Server.Port = %d, want %d", cfg.Server.Port, 9090)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("Log.Level = %q, want %q", cfg.Log.Level, "debug")
	}
	// Defaults preserved for fields not in YAML
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Server.Host = %q, want %q", cfg.Server.Host, "0.0.0.0")
	}
}

func TestLoad_EnvOverridesYAML(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
server:
  port: 9090
database:
  host: yamlhost
`
	path := writeYAML(t, yaml)

	t.Setenv("SHOPANDA_SERVER_PORT", "7070")
	t.Setenv("SHOPANDA_DATABASE_HOST", "envhost")
	t.Setenv("SHOPANDA_DATABASE_SSLMODE", "require")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Server.Port != 7070 {
		t.Errorf("Server.Port = %d, want %d", cfg.Server.Port, 7070)
	}
	if cfg.Database.Host != "envhost" {
		t.Errorf("Database.Host = %q, want %q", cfg.Database.Host, "envhost")
	}
}

func TestGet_DotNotation(t *testing.T) {
	yaml := `
server:
  host: "127.0.0.1"
  port: 3000
log:
  level: warn
`
	path := writeYAML(t, yaml)

	_, err := loadIsolated(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	tests := []struct {
		key  string
		want string
	}{
		{"server.host", "127.0.0.1"},
		{"server.port", "3000"},
		{"log.level", "warn"},
		{"nonexistent", ""},
	}

	for _, tt := range tests {
		got := Get(tt.key)
		if got != tt.want {
			t.Errorf("Get(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}

func TestGetOrDefault(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
server:
  port: 3000
`
	path := writeYAML(t, yaml)

	_, err := loadIsolated(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if v := GetOrDefault("server.port", "8080"); v != "3000" {
		t.Errorf("GetOrDefault existing = %q, want %q", v, "3000")
	}
	if v := GetOrDefault("missing.key", "fallback"); v != "fallback" {
		t.Errorf("GetOrDefault missing = %q, want %q", v, "fallback")
	}
}

func TestGetInt(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
server:
  port: 5000
`
	path := writeYAML(t, yaml)

	_, err := loadIsolated(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if v := GetInt("server.port"); v != 5000 {
		t.Errorf("GetInt = %d, want %d", v, 5000)
	}
	if v := GetInt("missing"); v != 0 {
		t.Errorf("GetInt missing = %d, want %d", v, 0)
	}
}

func TestDatabaseDSN(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
database:
  host: localhost
  port: 5432
  user: shopanda
  password: secret
  name: shopanda
  sslmode: disable
`
	path := writeYAML(t, yaml)

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	want := "postgres://shopanda:secret@localhost:5432/shopanda?sslmode=disable"
	if got := cfg.Database.DSN(); got != want {
		t.Errorf("DSN() = %q, want %q", got, want)
	}
}

func TestDatabaseDSN_EscapesReservedCharacters(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
database:
  host: 127.0.0.1
  port: 5432
  user: shop:anda
  password: s%v2M+aa
  name: shopanda
  sslmode: disable
`
	path := writeYAML(t, yaml)

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	want := "postgres://shop%3Aanda:s%25v2M+aa@127.0.0.1:5432/shopanda?sslmode=disable"
	if got := cfg.Database.DSN(); got != want {
		t.Errorf("DSN() = %q, want %q", got, want)
	}
}

func TestDatabaseDSN_IPv6Host(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
database:
  host: "::1"
  port: 5432
  user: shopanda
  password: secret
  name: shopanda
  sslmode: disable
`
	path := writeYAML(t, yaml)

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	want := "postgres://shopanda:secret@[::1]:5432/shopanda?sslmode=disable"
	if got := cfg.Database.DSN(); got != want {
		t.Errorf("DSN() = %q, want %q", got, want)
	}
}

func TestDatabaseDSN_EnvOverride(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, "")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	t.Setenv("DATABASE_URL", "postgres://override:5433/other")

	got := DatabaseDSN(cfg)
	if got != "postgres://override:5433/other" {
		t.Errorf("DatabaseDSN() = %q, want env override", got)
	}
}

func TestDatabaseDSN_EnvOverride_RepairsInvalidUserinfo(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, "")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	t.Setenv("DATABASE_URL", "postgres://shopanda:s%v2M+aa@127.0.0.1:5432/shopanda?sslmode=disable")

	want := "postgres://shopanda:s%25v2M+aa@127.0.0.1:5432/shopanda?sslmode=disable"
	if got := DatabaseDSN(cfg); got != want {
		t.Errorf("DatabaseDSN() = %q, want %q", got, want)
	}
}

func TestDatabaseDSN_EnvOverride_RepairFailureFallsBackToRaw(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, "")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	t.Setenv("DATABASE_URL", "notaurl")

	want := "notaurl"
	if got := DatabaseDSN(cfg); got != want {
		t.Errorf("DatabaseDSN() = %q, want %q", got, want)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := loadIsolated(t, filepath.Join(dir, "config.yaml"))
	if err == nil {
		t.Error("Load() expected error for missing file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	path := writeYAML(t, "{{invalid yaml")

	_, err := loadIsolated(t, path)
	if err == nil {
		t.Error("Load() expected error for invalid YAML")
	}
}

func TestConfigString_RedactsPassword(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
database:
  password: supersecret
`
	path := writeYAML(t, yaml)

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	s := cfg.String()
	if strings.Contains(s, "supersecret") {
		t.Error("String() should not contain the actual password")
	}
	if !strings.Contains(s, "***") {
		t.Error("String() should contain redacted password marker")
	}
}

// writeYAML creates a temp config file and returns its path.
func writeYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if content == "" {
		content = "# empty config — defaults apply\n"
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

// loadCfg is a test helper that calls Load and unwraps the Config from the result.
// It uses t.Chdir so the CWD .env fallback cannot pick up stray files.
func loadCfg(t *testing.T, path string) (*Config, error) {
	t.Helper()
	ensureTestJWTSecret(t)
	t.Chdir(filepath.Dir(path))
	res, err := Load(path)
	if err != nil {
		return nil, err
	}
	return res.Config, nil
}

// loadIsolated calls Load in an isolated CWD so the .env fallback cannot
// reach the developer's checkout. Use this instead of bare Load(path) calls.
func loadIsolated(t *testing.T, path string) (*LoadResult, error) {
	t.Helper()
	ensureTestJWTSecret(t)
	t.Chdir(filepath.Dir(path))
	return Load(path)
}

func ensureTestJWTSecret(t *testing.T) {
	t.Helper()
	// Always override — a weak leftover env must not break unrelated Load tests.
	t.Setenv("SHOPANDA_AUTH_JWT_SECRET", jwttest.TestSecret)
	ensureTestDevMode(t)
}

func ensureTestDevMode(t *testing.T) {
	t.Helper()
	// Allow default sslmode=disable on localhost unless the test already set DEV_MODE.
	if _, set := os.LookupEnv("SHOPANDA_DEV_MODE"); !set {
		t.Setenv("SHOPANDA_DEV_MODE", "true")
	}
}

func TestWebhooksConfig_SecretFromYAML(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
webhooks:
  secrets:
    stripe: "whsec_abc123"
    paypal: "pp_secret_xyz"
`
	path := writeYAML(t, yaml)

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if got := cfg.Webhooks.Secret("stripe"); got != "whsec_abc123" {
		t.Errorf("Secret(stripe) = %q, want %q", got, "whsec_abc123")
	}
	if got := cfg.Webhooks.Secret("paypal"); got != "pp_secret_xyz" {
		t.Errorf("Secret(paypal) = %q, want %q", got, "pp_secret_xyz")
	}
	if got := cfg.Webhooks.Secret("unknown"); got != "" {
		t.Errorf("Secret(unknown) = %q, want empty", got)
	}
}

func TestWebhooksConfig_SecretFromEnv(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, "")

	t.Setenv("SHOPANDA_WEBHOOKS_SECRET_STRIPE", "env_stripe_secret")
	t.Setenv("SHOPANDA_WEBHOOKS_SECRET_MANUAL", "env_manual_secret")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if got := cfg.Webhooks.Secret("stripe"); got != "env_stripe_secret" {
		t.Errorf("Secret(stripe) = %q, want %q", got, "env_stripe_secret")
	}
	if got := cfg.Webhooks.Secret("manual"); got != "env_manual_secret" {
		t.Errorf("Secret(manual) = %q, want %q", got, "env_manual_secret")
	}
}

func TestWebhooksConfig_EnvOverridesYAML(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
webhooks:
  secrets:
    stripe: "yaml_secret"
`
	path := writeYAML(t, yaml)

	t.Setenv("SHOPANDA_WEBHOOKS_SECRET_STRIPE", "env_override")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if got := cfg.Webhooks.Secret("stripe"); got != "env_override" {
		t.Errorf("Secret(stripe) = %q, want %q", got, "env_override")
	}
}

func TestWebhooksConfig_FlattenIncludesSecrets(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
webhooks:
  secrets:
    stripe: "whsec_flat"
`
	path := writeYAML(t, yaml)

	_, err := loadIsolated(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if got := Get("webhooks.secrets.stripe"); got != "whsec_flat" {
		t.Errorf("Get(webhooks.secrets.stripe) = %q, want %q", got, "whsec_flat")
	}
}

func TestLoad_CacheDriverDefault(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, "")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Cache.Driver != "postgres" {
		t.Errorf("Cache.Driver = %q, want %q", cfg.Cache.Driver, "postgres")
	}
}

func TestLoad_CacheDriverEnvOverlay(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, "")

	t.Setenv("SHOPANDA_CACHE_DRIVER", "redis")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Cache.Driver != "redis" {
		t.Errorf("Cache.Driver = %q, want %q", cfg.Cache.Driver, "redis")
	}

	if v := Get("cache.driver"); v != "redis" {
		t.Errorf("Get(\"cache.driver\") = %q, want %q", v, "redis")
	}
}

func TestLoad_QueueDriverDefault(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, "")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Queue.Driver != "postgres" {
		t.Errorf("Queue.Driver = %q, want postgres", cfg.Queue.Driver)
	}
}

func TestLoad_QueueDriverEnvOverlay(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, "")

	t.Setenv("SHOPANDA_QUEUE_DRIVER", "rabbitmq")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Queue.Driver != "rabbitmq" {
		t.Errorf("Queue.Driver = %q, want rabbitmq", cfg.Queue.Driver)
	}
}

func TestLoad_InvalidQueueDriver(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, `
queue:
  driver: invalid
`)

	_, err := loadIsolated(t, path)
	if err == nil {
		t.Fatal("Load() expected error for invalid queue.driver")
	}
	if !strings.Contains(err.Error(), "queue.driver") {
		t.Errorf("error = %q, want mention of queue.driver", err)
	}
}

func TestLoad_InvalidCacheDriver(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, `
cache:
  driver: memcached
`)

	_, err := loadIsolated(t, path)
	if err == nil {
		t.Fatal("Load() expected error for invalid cache.driver")
	}
	if !strings.Contains(err.Error(), "cache.driver") {
		t.Errorf("error = %q, want mention of cache.driver", err)
	}
}

func TestLoad_InvalidMediaStorage(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, `
media:
  storage: gcs
`)

	_, err := loadIsolated(t, path)
	if err == nil {
		t.Fatal("Load() expected error for invalid media.storage")
	}
	if !strings.Contains(err.Error(), "media.storage") {
		t.Errorf("error = %q, want mention of media.storage", err)
	}
}

func TestConfigString_ContainsQueueDriver(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, "")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	s := cfg.String()
	if !strings.Contains(s, "queue.driver=postgres") {
		t.Errorf("String() = %q, should contain queue.driver=postgres", s)
	}
}

func TestConfigString_ContainsCacheDriver(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, "")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	s := cfg.String()
	if !strings.Contains(s, "cache.driver=postgres") {
		t.Errorf("String() = %q, should contain cache.driver=postgres", s)
	}
}

func TestLoad_PluginsExampleDisabledByDefault(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, "")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Plugins.Example.Enabled {
		t.Fatal("Plugins.Example.Enabled = true, want false by default")
	}
	if cfg.Plugins.GraphQL.Enabled {
		t.Fatal("Plugins.GraphQL.Enabled = true, want false by default")
	}
	if cfg.Plugins.Example.FeeMinorUnits != 0 {
		t.Fatalf("Plugins.Example.FeeMinorUnits = %d, want 0 by default", cfg.Plugins.Example.FeeMinorUnits)
	}
}

func TestLoad_PublicBaseURL_RejectsWildcardHost(t *testing.T) {
	path := writeYAML(t, "")

	_, err := loadIsolated(t, path)
	if err == nil {
		t.Fatal("Load() expected error for wildcard bind host without public_base_url")
	}
	if !strings.Contains(err.Error(), "wildcard bind address") {
		t.Errorf("error = %q, want mention of wildcard bind address", err)
	}
}

func TestLoad_PublicBaseURL_FallsBackFromNonWildcardHost(t *testing.T) {
	yaml := `
server:
  host: "127.0.0.1"
  port: 3000
`
	path := writeYAML(t, yaml)

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	want := "http://127.0.0.1:3000"
	if cfg.Server.PublicBaseURL != want {
		t.Errorf("PublicBaseURL = %q, want %q", cfg.Server.PublicBaseURL, want)
	}
}

func TestLoad_PublicBaseURL_DefaultsSchemeToHTTPS(t *testing.T) {
	yaml := `
server:
  public_base_url: "shop.example.com"
`
	path := writeYAML(t, yaml)

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	want := "https://shop.example.com"
	if cfg.Server.PublicBaseURL != want {
		t.Errorf("PublicBaseURL = %q, want %q", cfg.Server.PublicBaseURL, want)
	}
}

func TestLoad_PublicBaseURL_StripsTrailingSlash(t *testing.T) {
	yaml := `
server:
  public_base_url: "https://shop.example.com/"
`
	path := writeYAML(t, yaml)

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	want := "https://shop.example.com"
	if cfg.Server.PublicBaseURL != want {
		t.Errorf("PublicBaseURL = %q, want %q", cfg.Server.PublicBaseURL, want)
	}
}

func TestLoad_PublicBaseURL_PreservesExplicitScheme(t *testing.T) {
	yaml := `
server:
  public_base_url: "http://localhost:3000"
`
	path := writeYAML(t, yaml)

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	want := "http://localhost:3000"
	if cfg.Server.PublicBaseURL != want {
		t.Errorf("PublicBaseURL = %q, want %q", cfg.Server.PublicBaseURL, want)
	}
}

func TestLoad_PublicBaseURL_EnvOverride(t *testing.T) {
	path := writeYAML(t, "")

	t.Setenv("SHOPANDA_SERVER_PUBLIC_BASE_URL", "shop.example.com/")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	want := "https://shop.example.com"
	if cfg.Server.PublicBaseURL != want {
		t.Errorf("PublicBaseURL = %q, want %q", cfg.Server.PublicBaseURL, want)
	}
}

func TestLoad_PublicBaseURL_FlattenedKey(t *testing.T) {
	yaml := `
server:
  public_base_url: "https://shop.example.com"
`
	path := writeYAML(t, yaml)

	_, err := loadIsolated(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if v := Get("server.public_base_url"); v != "https://shop.example.com" {
		t.Errorf("Get(server.public_base_url) = %q, want %q", v, "https://shop.example.com")
	}
}

func TestLoad_PublicBaseURL_RejectsUnsupportedScheme(t *testing.T) {
	yaml := `
server:
  public_base_url: "ftp://shop.example.com"
`
	path := writeYAML(t, yaml)

	_, err := loadIsolated(t, path)
	if err == nil {
		t.Fatal("Load() expected error for unsupported scheme")
	}
	if !strings.Contains(err.Error(), "unsupported scheme") {
		t.Errorf("error = %q, want mention of unsupported scheme", err)
	}
}

func TestLoad_PublicBaseURL_RejectsQuery(t *testing.T) {
	yaml := `
server:
  public_base_url: "https://shop.example.com?foo=bar"
`
	path := writeYAML(t, yaml)

	_, err := loadIsolated(t, path)
	if err == nil {
		t.Fatal("Load() expected error for query in URL")
	}
	if !strings.Contains(err.Error(), "query or fragment") {
		t.Errorf("error = %q, want mention of query or fragment", err)
	}
}

func TestLoad_PublicBaseURL_RejectsFragment(t *testing.T) {
	yaml := `
server:
  public_base_url: "https://shop.example.com#section"
`
	path := writeYAML(t, yaml)

	_, err := loadIsolated(t, path)
	if err == nil {
		t.Fatal("Load() expected error for fragment in URL")
	}
	if !strings.Contains(err.Error(), "query or fragment") {
		t.Errorf("error = %q, want mention of query or fragment", err)
	}
}

func TestLoad_PublicBaseURL_PreservesPath(t *testing.T) {
	yaml := `
server:
  public_base_url: "https://example.com/shop/"
`
	path := writeYAML(t, yaml)

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	want := "https://example.com/shop"
	if cfg.Server.PublicBaseURL != want {
		t.Errorf("PublicBaseURL = %q, want %q", cfg.Server.PublicBaseURL, want)
	}
}

func TestRateLimitConfig_FromYAML(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
rate_limit:
  enabled: true
  default:
    rate: 50
    burst: 100
  per_route:
    - path_prefix: "/api/v1/auth"
      rate: 5
      burst: 10
`
	path := writeYAML(t, yaml)

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !cfg.RateLimit.Enabled {
		t.Error("RateLimit.Enabled = false, want true")
	}
	if cfg.RateLimit.Default.Rate != 50 {
		t.Errorf("Default.Rate = %v, want 50", cfg.RateLimit.Default.Rate)
	}
	if cfg.RateLimit.Default.Burst != 100 {
		t.Errorf("Default.Burst = %d, want 100", cfg.RateLimit.Default.Burst)
	}
	if len(cfg.RateLimit.PerRoute) != 1 {
		t.Fatalf("PerRoute len = %d, want 1", len(cfg.RateLimit.PerRoute))
	}
	pr := cfg.RateLimit.PerRoute[0]
	if pr.PathPrefix != "/api/v1/auth" {
		t.Errorf("PerRoute[0].PathPrefix = %q, want %q", pr.PathPrefix, "/api/v1/auth")
	}
	if pr.Rate != 5 {
		t.Errorf("PerRoute[0].Rate = %v, want 5", pr.Rate)
	}
	if pr.Burst != 10 {
		t.Errorf("PerRoute[0].Burst = %d, want 10", pr.Burst)
	}
}

func TestRateLimitConfig_EnvOverlay(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, "")

	t.Setenv("SHOPANDA_RATE_LIMIT_ENABLED", "true")
	t.Setenv("SHOPANDA_RATE_LIMIT_DEFAULT_RATE", "25")
	t.Setenv("SHOPANDA_RATE_LIMIT_DEFAULT_BURST", "50")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !cfg.RateLimit.Enabled {
		t.Error("RateLimit.Enabled = false, want true from env")
	}
	if cfg.RateLimit.Default.Rate != 25 {
		t.Errorf("Default.Rate = %v, want 25 from env", cfg.RateLimit.Default.Rate)
	}
	if cfg.RateLimit.Default.Burst != 50 {
		t.Errorf("Default.Burst = %d, want 50 from env", cfg.RateLimit.Default.Burst)
	}
}

func TestRateLimitConfig_FlattenEntries(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
rate_limit:
  enabled: true
  default:
    rate: 10
    burst: 20
`
	path := writeYAML(t, yaml)

	_, err := loadIsolated(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if got := Get("rate_limit.enabled"); got != "true" {
		t.Errorf("Get(rate_limit.enabled) = %q, want %q", got, "true")
	}
	if got := Get("rate_limit.default.rate"); got != "10" {
		t.Errorf("Get(rate_limit.default.rate) = %q, want %q", got, "10")
	}
	if got := Get("rate_limit.default.burst"); got != "20" {
		t.Errorf("Get(rate_limit.default.burst) = %q, want %q", got, "20")
	}
}

func TestRateLimitConfig_EnvOverlay_IgnoresNonPositive(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
rate_limit:
  enabled: true
  default:
    rate: 50
    burst: 100
`
	path := writeYAML(t, yaml)

	// Non-positive values should be ignored.
	t.Setenv("SHOPANDA_RATE_LIMIT_DEFAULT_RATE", "0")
	t.Setenv("SHOPANDA_RATE_LIMIT_DEFAULT_BURST", "-5")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.RateLimit.Default.Rate != 50 {
		t.Errorf("Default.Rate = %v, want 50 (non-positive env should be ignored)", cfg.RateLimit.Default.Rate)
	}
	if cfg.RateLimit.Default.Burst != 100 {
		t.Errorf("Default.Burst = %d, want 100 (non-positive env should be ignored)", cfg.RateLimit.Default.Burst)
	}
}

func TestAuthLockoutConfig_EnvOverlay(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, "")

	t.Setenv("SHOPANDA_AUTH_LOCKOUT_ENABLED", "false")
	t.Setenv("SHOPANDA_AUTH_LOCKOUT_STORE", "memory")
	t.Setenv("SHOPANDA_AUTH_LOCKOUT_MAX_FAILURES", "7")
	t.Setenv("SHOPANDA_AUTH_LOCKOUT_WINDOW", "30m")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Auth.Lockout.Enabled {
		t.Error("Lockout.Enabled = true, want false from env")
	}
	if cfg.Auth.Lockout.Store != "memory" {
		t.Errorf("Lockout.Store = %q, want memory", cfg.Auth.Lockout.Store)
	}
	if cfg.Auth.Lockout.MaxFailures != 7 {
		t.Errorf("Lockout.MaxFailures = %d, want 7", cfg.Auth.Lockout.MaxFailures)
	}
	if cfg.Auth.Lockout.Window != "30m" {
		t.Errorf("Lockout.Window = %q, want 30m", cfg.Auth.Lockout.Window)
	}
}

func TestAuthJWTSecret_RejectsEmptyAndShort(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, "")
	t.Chdir(filepath.Dir(path))

	for _, secret := range []string{"", "short", "0123456789abcdef"} {
		t.Setenv("SHOPANDA_AUTH_JWT_SECRET", secret)
		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), jwt.EnvJWTSecret) {
			t.Fatalf("secret=%q: err=%v, want %s", secret, err, jwt.EnvJWTSecret)
		}
	}
}

func TestAuthJWTSecret_AcceptsInstallerHex(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, "")
	t.Chdir(filepath.Dir(path))
	// Load directly — loadCfg/ensureTestJWTSecret would overwrite the newline secret.
	t.Setenv("SHOPANDA_AUTH_JWT_SECRET", jwttest.TestSecret+"\n")

	res, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if res.Config.Auth.JWTSecret != jwttest.TestSecret {
		t.Fatalf("JWTSecret = %q, want trimmed installer hex", res.Config.Auth.JWTSecret)
	}
}

func TestAuthLockoutConfig_InvalidStore(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, `
auth:
  lockout:
    store: redis
`)
	_, err := loadCfg(t, path)
	if err == nil {
		t.Fatal("expected error for unsupported lockout store")
	}
}

func TestAuthLockoutConfig_InvalidWindow(t *testing.T) {
	withTestBaseURL(t)
	for _, window := range []string{"15x", "0s"} {
		path := writeYAML(t, fmt.Sprintf(`
auth:
  lockout:
    window: %q
`, window))
		_, err := loadCfg(t, path)
		if err == nil {
			t.Fatalf("window %q: expected error", window)
		}
	}
}

// --- dotenv tests ---

func TestLoadDotEnv_SetsUnsetVars(t *testing.T) {
	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	if err := os.WriteFile(dotenv, []byte("SHOPANDA_TEST_DOTENV_A=hello\nSHOPANDA_TEST_DOTENV_B=world\n"), 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	// Register for cleanup via t.Setenv, then unset so loadDotEnv can populate.
	t.Setenv("SHOPANDA_TEST_DOTENV_A", "")
	os.Unsetenv("SHOPANDA_TEST_DOTENV_A")
	t.Setenv("SHOPANDA_TEST_DOTENV_B", "")
	os.Unsetenv("SHOPANDA_TEST_DOTENV_B")

	loaded, err := loadDotEnv(dotenv)
	if err != nil {
		t.Fatalf("loadDotEnv error: %v", err)
	}
	if !loaded {
		t.Fatal("loadDotEnv returned false, want true")
	}
	if got := os.Getenv("SHOPANDA_TEST_DOTENV_A"); got != "hello" {
		t.Errorf("SHOPANDA_TEST_DOTENV_A = %q, want %q", got, "hello")
	}
	if got := os.Getenv("SHOPANDA_TEST_DOTENV_B"); got != "world" {
		t.Errorf("SHOPANDA_TEST_DOTENV_B = %q, want %q", got, "world")
	}
}

func TestLoadDotEnv_OSEnvTakesPrecedence(t *testing.T) {
	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	if err := os.WriteFile(dotenv, []byte("SHOPANDA_TEST_DOTENV_PRIO=from_file\n"), 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	t.Setenv("SHOPANDA_TEST_DOTENV_PRIO", "from_os")

	if _, err := loadDotEnv(dotenv); err != nil {
		t.Fatalf("loadDotEnv error: %v", err)
	}

	if got := os.Getenv("SHOPANDA_TEST_DOTENV_PRIO"); got != "from_os" {
		t.Errorf("got %q, want %q — OS env should win over .env file", got, "from_os")
	}
}

func TestLoadDotEnv_MissingFileReturnsFalse(t *testing.T) {
	loaded, err := loadDotEnv(filepath.Join(t.TempDir(), ".env"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loaded {
		t.Error("loadDotEnv should return false for missing file")
	}
}

func TestLoadDotEnv_SkipsCommentsAndBlanks(t *testing.T) {
	dir := t.TempDir()
	dotenv := filepath.Join(dir, ".env")
	content := "# comment\n\n  \nSHOPANDA_TEST_DOTENV_C=value\n# another comment\n"
	if err := os.WriteFile(dotenv, []byte(content), 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	t.Setenv("SHOPANDA_TEST_DOTENV_C", "")
	os.Unsetenv("SHOPANDA_TEST_DOTENV_C")

	if _, err := loadDotEnv(dotenv); err != nil {
		t.Fatalf("loadDotEnv error: %v", err)
	}
	if got := os.Getenv("SHOPANDA_TEST_DOTENV_C"); got != "value" {
		t.Errorf("got %q, want %q", got, "value")
	}
}

func TestParseDotEnvLine(t *testing.T) {
	tests := []struct {
		line    string
		wantKey string
		wantVal string
		wantOK  bool
	}{
		{`KEY=value`, "KEY", "value", true},
		{`KEY="quoted value"`, "KEY", "quoted value", true},
		{`KEY='single quoted'`, "KEY", "single quoted", true},
		{`export KEY=exported`, "KEY", "exported", true},
		{`KEY=`, "KEY", "", true},
		{`KEY=val=ue`, "KEY", "val=ue", true},
		{`# comment`, "", "", false},
		{`no_equals`, "", "", false},
		{`=no_key`, "", "", false},
	}

	for _, tt := range tests {
		key, val, ok := parseDotEnvLine(tt.line)
		if ok != tt.wantOK {
			t.Errorf("parseDotEnvLine(%q) ok = %v, want %v", tt.line, ok, tt.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if key != tt.wantKey {
			t.Errorf("parseDotEnvLine(%q) key = %q, want %q", tt.line, key, tt.wantKey)
		}
		if val != tt.wantVal {
			t.Errorf("parseDotEnvLine(%q) val = %q, want %q", tt.line, val, tt.wantVal)
		}
	}
}

func TestLoad_FrontendStrictSlotMarkers_EnvOverlay(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, "")

	t.Run("true", func(t *testing.T) {
		t.Setenv("SHOPANDA_FRONTEND_STRICT_SLOT_MARKERS", "true")
		cfg, err := loadCfg(t, path)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if !cfg.Frontend.StrictSlotMarkers {
			t.Fatal("StrictSlotMarkers = false, want true from env")
		}
	})

	t.Run("one", func(t *testing.T) {
		t.Setenv("SHOPANDA_FRONTEND_STRICT_SLOT_MARKERS", "1")
		cfg, err := loadCfg(t, path)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if !cfg.Frontend.StrictSlotMarkers {
			t.Fatal("StrictSlotMarkers = false, want true from env")
		}
	})

	t.Run("other values unchanged", func(t *testing.T) {
		for _, v := range []string{"false", "0", "banana"} {
			t.Setenv("SHOPANDA_FRONTEND_STRICT_SLOT_MARKERS", v)
			cfg, err := loadCfg(t, path)
			if err != nil {
				t.Fatalf("Load() env=%q error: %v", v, err)
			}
			if cfg.Frontend.StrictSlotMarkers {
				t.Fatalf("StrictSlotMarkers = true for env %q, want false", v)
			}
		}
	})
}

func TestFrontendStrictSlotMarkers_FlattenEntries(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
frontend:
  strict_slot_markers: true
`
	path := writeYAML(t, yaml)

	_, err := loadIsolated(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if got := Get("frontend.strict_slot_markers"); got != "true" {
		t.Errorf("Get(frontend.strict_slot_markers) = %q, want %q", got, "true")
	}
}

func TestLoad_DotEnvUsedTrue(t *testing.T) {
	withTestBaseURL(t)
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("# empty\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	dotenv := filepath.Join(dir, ".env")
	if err := os.WriteFile(dotenv, []byte("SHOPANDA_LOG_LEVEL=debug\n"), 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	// Scope the side-effect: loadDotEnv os.Setenv's SHOPANDA_LOG_LEVEL.
	t.Setenv("SHOPANDA_LOG_LEVEL", "")
	os.Unsetenv("SHOPANDA_LOG_LEVEL")

	// Isolate CWD so the fallback doesn't pick up stray .env files.
	t.Chdir(dir)

	result, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !result.DotEnvUsed {
		t.Error("DotEnvUsed = false, want true when .env exists")
	}
	if result.Config.Log.Level != "debug" {
		t.Errorf("Log.Level = %q, want %q from .env", result.Config.Log.Level, "debug")
	}
}

func TestLoad_DotEnvUsedFalse(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, "")

	// Isolate CWD so the fallback doesn't find a stray .env.
	t.Chdir(filepath.Dir(path))

	result, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if result.DotEnvUsed {
		t.Error("DotEnvUsed = true, want false when no .env exists")
	}
}

func TestMetricsConfig_DefaultsDisabled(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, "")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Metrics.Enabled {
		t.Error("Metrics.Enabled = true, want false by default")
	}
	if cfg.Metrics.Listen != DefaultMetricsListen {
		t.Errorf("Metrics.Listen = %q, want %q", cfg.Metrics.Listen, DefaultMetricsListen)
	}
}

func TestMetricsConfig_FromYAML(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
metrics:
  enabled: true
  listen: "0.0.0.0:9999"
`
	path := writeYAML(t, yaml)

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !cfg.Metrics.Enabled {
		t.Error("Metrics.Enabled = false, want true")
	}
	if cfg.Metrics.Listen != "0.0.0.0:9999" {
		t.Errorf("Metrics.Listen = %q, want %q", cfg.Metrics.Listen, "0.0.0.0:9999")
	}
}

func TestMetricsConfig_EnabledWithoutListen_DefaultsToLoopback(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
metrics:
  enabled: true
  listen: ""
`
	path := writeYAML(t, yaml)

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Metrics.Listen != DefaultMetricsListen {
		t.Errorf("Metrics.Listen = %q, want %q (defensive default when enabled with a blank listen)", cfg.Metrics.Listen, DefaultMetricsListen)
	}
}

func TestMetricsConfig_EnvOverlay(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, "")

	t.Setenv("SHOPANDA_METRICS_ENABLED", "true")
	t.Setenv("SHOPANDA_METRICS_LISTEN", "127.0.0.1:9091")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !cfg.Metrics.Enabled {
		t.Error("Metrics.Enabled = false, want true from env")
	}
	if cfg.Metrics.Listen != "127.0.0.1:9091" {
		t.Errorf("Metrics.Listen = %q, want %q from env", cfg.Metrics.Listen, "127.0.0.1:9091")
	}
}

func TestMetricsConfig_FlattenEntries(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
metrics:
  enabled: true
  listen: "127.0.0.1:9090"
`
	path := writeYAML(t, yaml)

	_, err := loadIsolated(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if got := Get("metrics.enabled"); got != "true" {
		t.Errorf("Get(metrics.enabled) = %q, want %q", got, "true")
	}
	if got := Get("metrics.listen"); got != "127.0.0.1:9090" {
		t.Errorf("Get(metrics.listen) = %q, want %q", got, "127.0.0.1:9090")
	}
}

func TestTracingConfig_DefaultsDisabled(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, "")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Tracing.Enabled {
		t.Error("Tracing.Enabled = true, want false by default")
	}
}

func TestTracingConfig_FromYAML(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
tracing:
  enabled: true
  endpoint: "collector.example.com:4318"
  insecure: false
  sample_ratio: 0.25
  headers:
    x-api-key: "secret"
`
	path := writeYAML(t, yaml)

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !cfg.Tracing.Enabled {
		t.Error("Tracing.Enabled = false, want true")
	}
	if cfg.Tracing.Endpoint != "collector.example.com:4318" {
		t.Errorf("Tracing.Endpoint = %q, want %q", cfg.Tracing.Endpoint, "collector.example.com:4318")
	}
	if cfg.Tracing.SampleRatio != 0.25 {
		t.Errorf("Tracing.SampleRatio = %v, want 0.25", cfg.Tracing.SampleRatio)
	}
	if cfg.Tracing.Headers["x-api-key"] != "secret" {
		t.Errorf("Tracing.Headers[x-api-key] = %q, want %q", cfg.Tracing.Headers["x-api-key"], "secret")
	}
}

func TestTracingConfig_EnabledWithoutEndpoint_Rejected(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
tracing:
  enabled: true
`
	path := writeYAML(t, yaml)

	_, err := loadCfg(t, path)
	if err == nil || !strings.Contains(err.Error(), "tracing.endpoint") {
		t.Fatalf("err = %v, want a tracing.endpoint validation error", err)
	}
}

func TestTracingConfig_SampleRatioDefaultsTo1WhenEnabled(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
tracing:
  enabled: true
  endpoint: "collector.example.com:4318"
`
	path := writeYAML(t, yaml)

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Tracing.SampleRatio != 1.0 {
		t.Errorf("Tracing.SampleRatio = %v, want 1.0 default", cfg.Tracing.SampleRatio)
	}
}

func TestTracingConfig_SampleRatioClampedAboveOne(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
tracing:
  enabled: true
  endpoint: "collector.example.com:4318"
  sample_ratio: 150
`
	path := writeYAML(t, yaml)

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Tracing.SampleRatio != 1.0 {
		t.Errorf("Tracing.SampleRatio = %v, want clamped to 1.0", cfg.Tracing.SampleRatio)
	}
}

func TestTracingConfig_DisabledSkipsEndpointValidation(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, "") // tracing.enabled defaults false, no endpoint set

	if _, err := loadCfg(t, path); err != nil {
		t.Fatalf("Load() error: %v, want disabled tracing to skip endpoint validation", err)
	}
}

func TestTracingConfig_EnvOverlay(t *testing.T) {
	withTestBaseURL(t)
	path := writeYAML(t, "")

	t.Setenv("SHOPANDA_TRACING_ENABLED", "true")
	t.Setenv("SHOPANDA_TRACING_ENDPOINT", "otel.example.com:4318")
	t.Setenv("SHOPANDA_TRACING_INSECURE", "true")
	t.Setenv("SHOPANDA_TRACING_SAMPLE_RATIO", "0.5")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !cfg.Tracing.Enabled {
		t.Error("Tracing.Enabled = false, want true from env")
	}
	if cfg.Tracing.Endpoint != "otel.example.com:4318" {
		t.Errorf("Tracing.Endpoint = %q, want env value", cfg.Tracing.Endpoint)
	}
	if !cfg.Tracing.Insecure {
		t.Error("Tracing.Insecure = false, want true from env")
	}
	if cfg.Tracing.SampleRatio != 0.5 {
		t.Errorf("Tracing.SampleRatio = %v, want 0.5 from env", cfg.Tracing.SampleRatio)
	}
}

// TestTracingConfig_ExplicitZeroSampleRatioIsPreserved pins the fix
// distinguishing "operator never mentioned sample_ratio" (defaults to 1.0,
// see TestTracingConfig_SampleRatioDefaultsTo1WhenEnabled) from "operator
// wrote sample_ratio: 0" (a deliberate sample-nothing signal). Before the
// fix, both cases were indistinguishable float64 zero values and the
// normalizer forced either one up to 1.0 — silently sampling everything
// for an operator who explicitly asked for the opposite.
func TestTracingConfig_ExplicitZeroSampleRatioIsPreserved(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
tracing:
  enabled: true
  endpoint: "collector.example.com:4318"
  sample_ratio: 0
`
	path := writeYAML(t, yaml)

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Tracing.SampleRatio != 0 {
		t.Errorf("Tracing.SampleRatio = %v, want 0 preserved, not forced to 1.0", cfg.Tracing.SampleRatio)
	}
}

func TestTracingConfig_NegativeSampleRatioRejected(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
tracing:
  enabled: true
  endpoint: "collector.example.com:4318"
  sample_ratio: -0.5
`
	path := writeYAML(t, yaml)

	_, err := loadCfg(t, path)
	if err == nil || !strings.Contains(err.Error(), "sample_ratio") {
		t.Fatalf("err = %v, want a sample_ratio validation error", err)
	}
}

func TestTracingConfig_EndpointWithSchemeRejected(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
tracing:
  enabled: true
  endpoint: "https://collector.example.com:4318"
`
	path := writeYAML(t, yaml)

	_, err := loadCfg(t, path)
	if err == nil || !strings.Contains(err.Error(), "tracing.endpoint") {
		t.Fatalf("err = %v, want a tracing.endpoint scheme-rejection error", err)
	}
}

func TestTracingConfig_InsecureAgainstRemoteHostRejectedWithoutDevMode(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "false")
	t.Setenv("SHOPANDA_DATABASE_PASSWORD", "strong-enough-secret")
	yaml := `
database:
  host: localhost
  sslmode: require
tracing:
  enabled: true
  endpoint: "collector.example.com:4318"
  insecure: true
`
	path := writeYAML(t, yaml)

	_, err := loadCfg(t, path)
	if err == nil || !strings.Contains(err.Error(), "insecure") {
		t.Fatalf("err = %v, want an insecure-export validation error", err)
	}
}

func TestTracingConfig_InsecureAgainstRemoteHostAllowedWithDevMode(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "true")
	yaml := `
tracing:
  enabled: true
  endpoint: "collector.example.com:4318"
  insecure: true
`
	path := writeYAML(t, yaml)

	if _, err := loadCfg(t, path); err != nil {
		t.Fatalf("Load() error: %v, want SHOPANDA_DEV_MODE to permit insecure export against a remote host", err)
	}
}

func TestTracingConfig_InsecureAgainstLocalhostAllowedWithoutDevMode(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "false")
	t.Setenv("SHOPANDA_DATABASE_PASSWORD", "strong-enough-secret")
	yaml := `
database:
  host: localhost
  sslmode: require
tracing:
  enabled: true
  endpoint: "localhost:4318"
  insecure: true
`
	path := writeYAML(t, yaml)

	if _, err := loadCfg(t, path); err != nil {
		t.Fatalf("Load() error: %v, want a local collector endpoint to permit insecure export without dev mode", err)
	}
}

func TestTracingConfig_FlattenEntries(t *testing.T) {
	withTestBaseURL(t)
	yaml := `
tracing:
  enabled: true
  endpoint: "collector.example.com:4318"
  sample_ratio: 0.5
  headers:
    x-api-key: "top-secret"
`
	path := writeYAML(t, yaml)

	_, err := loadIsolated(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if got := Get("tracing.enabled"); got != "true" {
		t.Errorf("Get(tracing.enabled) = %q, want %q", got, "true")
	}
	if got := Get("tracing.endpoint"); got != "collector.example.com:4318" {
		t.Errorf("Get(tracing.endpoint) = %q, want %q", got, "collector.example.com:4318")
	}
	if got := Get("tracing.sample_ratio"); got != "0.5" {
		t.Errorf("Get(tracing.sample_ratio) = %q, want %q", got, "0.5")
	}
	// Must never surface the raw header value through Get()/GetOrDefault()
	// — it commonly carries a collector API key.
	if got := Get("tracing.headers.x-api-key"); got != "***" {
		t.Errorf("Get(tracing.headers.x-api-key) = %q, want redacted %q", got, "***")
	}
}
