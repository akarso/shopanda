package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withTestBaseURL sets a valid PublicBaseURL via env so tests that don't
// exercise PublicBaseURL logic are not affected by wildcard-host rejection.
func withTestBaseURL(t *testing.T) {
	t.Helper()
	t.Setenv("SHOPANDA_SERVER_PUBLIC_BASE_URL", "http://test.localhost:8080")
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
	t.Chdir(filepath.Dir(path))
	return Load(path)
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

	// Use a clearly fake value to exercise the env overlay without implying runtime support.
	t.Setenv("SHOPANDA_CACHE_DRIVER", "test-driver")

	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Cache.Driver != "test-driver" {
		t.Errorf("Cache.Driver = %q, want %q", cfg.Cache.Driver, "test-driver")
	}

	// Verify flattened key.
	if v := Get("cache.driver"); v != "test-driver" {
		t.Errorf("Get(\"cache.driver\") = %q, want %q", v, "test-driver")
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
