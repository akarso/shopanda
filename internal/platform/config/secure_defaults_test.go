package config

import (
	"strings"
	"testing"
)

func TestEnvTruthy(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"1", true},
		{"true", true},
		{"TRUE", true},
		{"yes", true},
		{"Yes", true},
		{"", false},
		{"0", false},
		{"false", false},
		{"no", false},
		{"anything", false},
	}
	for _, tc := range cases {
		t.Setenv("SHOPANDA_TEST_TRUTHY", tc.val)
		if got := EnvTruthy("SHOPANDA_TEST_TRUTHY"); got != tc.want {
			t.Errorf("EnvTruthy(%q) = %v, want %v", tc.val, got, tc.want)
		}
	}
}

func TestDevModeEnabled(t *testing.T) {
	t.Setenv("SHOPANDA_DEV_MODE", "true")
	if !DevModeEnabled() {
		t.Fatal("want DevModeEnabled true")
	}
	t.Setenv("SHOPANDA_DEV_MODE", "0")
	if DevModeEnabled() {
		t.Fatal("want DevModeEnabled false for 0")
	}
	t.Setenv("SHOPANDA_DEV_MODE", "")
	if DevModeEnabled() {
		t.Fatal("want DevModeEnabled false for empty")
	}
}

func TestShouldLogPasswordResetTokens(t *testing.T) {
	t.Setenv("SHOPANDA_DEV_MODE", "true")
	t.Setenv("SHOPANDA_DEV_LOG_RESET_TOKENS", "1")
	if !ShouldLogPasswordResetTokens() {
		t.Fatal("want true when both truthy")
	}
	t.Setenv("SHOPANDA_DEV_LOG_RESET_TOKENS", "")
	if ShouldLogPasswordResetTokens() {
		t.Fatal("want false when reset-token flag absent")
	}
	t.Setenv("SHOPANDA_DEV_MODE", "false")
	t.Setenv("SHOPANDA_DEV_LOG_RESET_TOKENS", "1")
	if ShouldLogPasswordResetTokens() {
		t.Fatal("want false when DEV_MODE falsey")
	}
}

func TestLoad_RejectsWeakPasswordWithoutDevMode(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "false")
	t.Setenv("SHOPANDA_DATABASE_PASSWORD", "changeme")
	path := writeYAML(t, `
database:
  host: localhost
  password: ignored
  sslmode: disable
`)
	_, err := loadCfg(t, path)
	if err == nil || !strings.Contains(err.Error(), "forbidden default") {
		t.Fatalf("err=%v, want forbidden default", err)
	}
}

func TestLoad_RejectsWeakPasswordWhenDevModeAbsent(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "")
	t.Setenv("SHOPANDA_DATABASE_PASSWORD", "shopanda")
	path := writeYAML(t, `
database:
  host: localhost
  sslmode: disable
`)
	_, err := loadCfg(t, path)
	if err == nil || !strings.Contains(err.Error(), "forbidden default") {
		t.Fatalf("err=%v, want forbidden default when DEV_MODE empty", err)
	}
}

func TestLoad_AllowsWeakPasswordWithDevMode(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "true")
	t.Setenv("SHOPANDA_DATABASE_PASSWORD", "changeme")
	path := writeYAML(t, `
database:
  host: localhost
  sslmode: disable
`)
	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.Password != "changeme" {
		t.Fatalf("password = %q", cfg.Database.Password)
	}
}

func TestLoad_RejectsMetricsWildcardWithoutDevMode(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "false")
	t.Setenv("SHOPANDA_DATABASE_PASSWORD", "strong-enough-secret")
	path := writeYAML(t, `
database:
  host: localhost
  sslmode: require
metrics:
  enabled: true
  listen: "0.0.0.0:9090"
`)
	_, err := loadCfg(t, path)
	if err == nil || !strings.Contains(err.Error(), "metrics.listen") {
		t.Fatalf("err=%v, want metrics.listen rejection", err)
	}
}

func TestLoad_RejectsMetricsNonLoopbackWithoutDevMode(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "false")
	t.Setenv("SHOPANDA_DATABASE_PASSWORD", "strong-enough-secret")
	path := writeYAML(t, `
database:
  host: localhost
  sslmode: require
metrics:
  enabled: true
  listen: "10.0.0.5:9090"
`)
	_, err := loadCfg(t, path)
	if err == nil || !strings.Contains(err.Error(), "metrics.listen") {
		t.Fatalf("err=%v, want non-loopback metrics.listen rejection", err)
	}
}

func TestLoad_AllowsMetricsLoopbackWithoutDevMode(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "false")
	t.Setenv("SHOPANDA_DATABASE_PASSWORD", "strong-enough-secret")
	path := writeYAML(t, `
database:
  host: localhost
  sslmode: require
metrics:
  enabled: true
  listen: "127.0.0.1:9090"
`)
	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Metrics.Listen != "127.0.0.1:9090" {
		t.Errorf("Metrics.Listen = %q", cfg.Metrics.Listen)
	}
}

func TestLoad_AllowsMetricsWildcardWithDevMode(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "true")
	t.Setenv("SHOPANDA_DATABASE_PASSWORD", "strong-enough-secret")
	path := writeYAML(t, `
database:
  host: localhost
  sslmode: require
metrics:
  enabled: true
  listen: "0.0.0.0:9090"
`)
	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Metrics.Listen != "0.0.0.0:9090" {
		t.Errorf("Metrics.Listen = %q", cfg.Metrics.Listen)
	}
}

// TestLoad_MetricsInsecureBindFlagIsIndependentOfDevMode pins the fix for the
// two concerns being coupled: SHOPANDA_METRICS_ALLOW_INSECURE_BIND alone must
// allow a non-loopback metrics bind without also disabling the (unrelated)
// DB password/SSL checks that SHOPANDA_DEV_MODE gates.
func TestLoad_MetricsInsecureBindFlagIsIndependentOfDevMode(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "false")
	t.Setenv("SHOPANDA_METRICS_ALLOW_INSECURE_BIND", "true")
	t.Setenv("SHOPANDA_DATABASE_PASSWORD", "strong-enough-secret")
	path := writeYAML(t, `
database:
  host: localhost
  sslmode: require
metrics:
  enabled: true
  listen: "0.0.0.0:9090"
`)
	cfg, err := loadCfg(t, path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Metrics.Listen != "0.0.0.0:9090" {
		t.Errorf("Metrics.Listen = %q", cfg.Metrics.Listen)
	}

	// The same flag must not weaken the unrelated DB password check.
	// SHOPANDA_DATABASE_PASSWORD is re-set here (env overrides YAML) so the
	// forbidden default actually reaches validation instead of being masked
	// by the "strong-enough-secret" set at the top of this test.
	t.Setenv("SHOPANDA_DATABASE_PASSWORD", "changeme")
	path2 := writeYAML(t, `
database:
  host: localhost
  sslmode: require
metrics:
  enabled: true
  listen: "0.0.0.0:9090"
`)
	if _, err := loadCfg(t, path2); err == nil {
		t.Fatal("expected forbidden default DB password to still be rejected with SHOPANDA_METRICS_ALLOW_INSECURE_BIND=true")
	}
}

func TestLoad_RejectsSSLDisableWithoutDevMode(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "false")
	t.Setenv("SHOPANDA_DATABASE_PASSWORD", "strong-enough-secret")
	path := writeYAML(t, `
database:
  host: localhost
  sslmode: disable
`)
	_, err := loadCfg(t, path)
	if err == nil || !strings.Contains(err.Error(), "sslmode") {
		t.Fatalf("err=%v, want sslmode rejection", err)
	}
}

func TestLoad_RejectsSSLPreferWithoutLocalDev(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "false")
	t.Setenv("SHOPANDA_DATABASE_PASSWORD", "strong-enough-secret")
	path := writeYAML(t, `
database:
  host: db.example.com
  sslmode: prefer
`)
	_, err := loadCfg(t, path)
	if err == nil || !strings.Contains(err.Error(), "prefer") {
		t.Fatalf("err=%v, want prefer rejection", err)
	}
}

func TestLoad_RejectsSSLAllowWithoutLocalDev(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "true") // truthy alone is not enough on remote host
	t.Setenv("SHOPANDA_DATABASE_PASSWORD", "strong-enough-secret")
	path := writeYAML(t, `
database:
  host: db.example.com
  sslmode: allow
`)
	_, err := loadCfg(t, path)
	if err == nil || !strings.Contains(err.Error(), "allow") {
		t.Fatalf("err=%v, want allow rejection", err)
	}
}

func TestLoad_RejectsSSLDisableOnRemoteHostEvenWithDevMode(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "true")
	t.Setenv("SHOPANDA_DATABASE_PASSWORD", "strong-enough-secret")
	path := writeYAML(t, `
database:
  host: db.example.com
  sslmode: disable
`)
	_, err := loadCfg(t, path)
	if err == nil || !strings.Contains(err.Error(), "sslmode") {
		t.Fatalf("err=%v, want sslmode rejection for remote host", err)
	}
}

func TestLoad_AllowsSSLDisableOnComposePostgresWithDevMode(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "1")
	t.Setenv("SHOPANDA_DATABASE_PASSWORD", "strong-enough-secret")
	path := writeYAML(t, `
database:
  host: postgres
  sslmode: disable
`)
	if _, err := loadCfg(t, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestLoad_AllowsSSLRequireOnRemoteWithoutDevMode(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "false")
	t.Setenv("SHOPANDA_DATABASE_PASSWORD", "strong-enough-secret")
	path := writeYAML(t, `
database:
  host: db.example.com
  sslmode: require
`)
	if _, err := loadCfg(t, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestLoad_RejectsWeakPasswordInDATABASE_URL(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "false")
	t.Setenv("DATABASE_URL", "postgres://shopanda:changeme@db.example.com:5432/shopanda?sslmode=require")
	path := writeYAML(t, `
database:
  host: localhost
  password: strong-enough-secret
  sslmode: require
`)
	_, err := loadCfg(t, path)
	if err == nil || !strings.Contains(err.Error(), "forbidden default") {
		t.Fatalf("err=%v, want forbidden default from DATABASE_URL", err)
	}
}

func TestLoad_RejectsDATABASE_URLMissingSSLModeDespiteYAMLRequire(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "false")
	t.Setenv("SHOPANDA_DATABASE_SSLMODE", "require")
	t.Setenv("SHOPANDA_DATABASE_PASSWORD", "strong-enough-secret")
	// No sslmode in URL — must not inherit YAML/env require (DatabaseDSN returns raw URL).
	t.Setenv("DATABASE_URL", "postgres://u:strong-enough-secret@db.example.com:5432/shopanda")
	path := writeYAML(t, `
database:
  host: localhost
  password: strong-enough-secret
  sslmode: require
`)
	_, err := loadCfg(t, path)
	if err == nil || !strings.Contains(err.Error(), "sslmode") {
		t.Fatalf("err=%v, want missing-URL-sslmode treated as insecure", err)
	}
}

func TestLoad_RejectsLibpqKeywordDATABASE_URLWeakSecret(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "false")
	t.Setenv("SHOPANDA_DATABASE_PASSWORD", "strong-enough-secret")
	t.Setenv("SHOPANDA_DATABASE_SSLMODE", "require")
	t.Setenv("DATABASE_URL", "host=db.example.com user=u password=changeme dbname=shopanda sslmode=disable")
	path := writeYAML(t, `
database:
  host: localhost
  password: strong-enough-secret
  sslmode: require
`)
	_, err := loadCfg(t, path)
	if err == nil || !strings.Contains(err.Error(), "forbidden default") {
		t.Fatalf("err=%v, want forbidden default from libpq DATABASE_URL", err)
	}
}

func TestLoad_RejectsLibpqKeywordDATABASE_URLDisableSSL(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "false")
	t.Setenv("SHOPANDA_DATABASE_PASSWORD", "strong-enough-secret")
	t.Setenv("SHOPANDA_DATABASE_SSLMODE", "require")
	t.Setenv("DATABASE_URL", "host=db.example.com user=u password=strong-enough-secret dbname=shopanda sslmode=disable")
	path := writeYAML(t, `
database:
  host: localhost
  password: strong-enough-secret
  sslmode: require
`)
	_, err := loadCfg(t, path)
	if err == nil || !strings.Contains(err.Error(), "sslmode") {
		t.Fatalf("err=%v, want sslmode rejection from libpq DATABASE_URL", err)
	}
}

func TestLoad_AllowsDATABASE_URLWithRequire(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "false")
	t.Setenv("DATABASE_URL", "postgres://u:strong-enough-secret@db.example.com:5432/shopanda?sslmode=require")
	path := writeYAML(t, `
database:
  host: localhost
  password: changeme
  sslmode: disable
`)
	if _, err := loadCfg(t, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestLoad_RejectsDATABASE_URLHostaddrRemoteWithLocalHost(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "true")
	// host=localhost would look local, but hostaddr is the dial target (libpq).
	t.Setenv("DATABASE_URL", "postgres://u:strong-enough-secret@localhost:5432/shopanda?hostaddr=203.0.113.10&sslmode=disable")
	path := writeYAML(t, "")
	_, err := loadCfg(t, path)
	if err == nil || !strings.Contains(err.Error(), "sslmode") {
		t.Fatalf("err=%v, want sslmode rejection when hostaddr is remote", err)
	}
}

func TestLoad_RejectsDATABASE_URLConflictingSSLMode(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "false")
	t.Setenv("DATABASE_URL", "postgres://u:strong-enough-secret@db.example.com:5432/shopanda?sslmode=require&sslmode=disable")
	path := writeYAML(t, "")
	_, err := loadCfg(t, path)
	if err == nil || !strings.Contains(err.Error(), "conflicting sslmode") {
		t.Fatalf("err=%v, want conflicting sslmode rejection", err)
	}
}

func TestLoad_RejectsLibpqHostaddrRemoteOverride(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "true")
	t.Setenv("DATABASE_URL", "hostaddr=203.0.113.10 host=localhost password=strong-enough-secret dbname=shopanda sslmode=disable")
	path := writeYAML(t, "")
	_, err := loadCfg(t, path)
	if err == nil || !strings.Contains(err.Error(), "sslmode") {
		t.Fatalf("err=%v, want sslmode rejection for remote hostaddr", err)
	}
}

func TestLoad_RejectsInvalidDATABASE_URLForm(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "false")
	t.Setenv("DATABASE_URL", "not-a-dsn")
	path := writeYAML(t, "")
	_, err := loadCfg(t, path)
	if err == nil || !strings.Contains(err.Error(), "must be a postgres URL or libpq keyword/value DSN") {
		t.Fatalf("err=%v, want invalid DSN form rejection", err)
	}
}

func TestLoad_ParsesQuotedLibpqPassword(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "false")
	// Quoted password with space must not break field scanning / weak-secret check.
	t.Setenv("DATABASE_URL", "host=db.example.com user=u password='strong enough secret' dbname=shopanda sslmode=require")
	path := writeYAML(t, "")
	if _, err := loadCfg(t, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestLoad_ParsesURIPasswordWithEncodedSpace(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "false")
	t.Setenv("DATABASE_URL", "postgres://u:strong%20enough@db.example.com:5432/shopanda?sslmode=require")
	path := writeYAML(t, "")
	if _, err := loadCfg(t, path); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestLoad_RejectsSSLModeInjectedViaURIPassword(t *testing.T) {
	withTestBaseURL(t)
	t.Setenv("SHOPANDA_DEV_MODE", "false")
	// Password contains the substring sslmode=require; missing query sslmode must still fail.
	t.Setenv("DATABASE_URL", "postgres://u:evil%20sslmode=require@db.example.com:5432/shopanda")
	path := writeYAML(t, "")
	_, err := loadCfg(t, path)
	if err == nil || !strings.Contains(err.Error(), "sslmode") {
		t.Fatalf("err=%v, want sslmode rejection (password must not satisfy TLS policy)", err)
	}
}

func TestIsLoopbackHost_LiteralIPs(t *testing.T) {
	if !isLoopbackHost("127.0.0.1") {
		t.Error("127.0.0.1 should be loopback")
	}
	if !isLoopbackHost("::1") {
		t.Error("::1 should be loopback")
	}
	if isLoopbackHost("10.0.0.1") {
		t.Error("10.0.0.1 should not be loopback")
	}
	if isLoopbackHost("0.0.0.0") {
		t.Error("0.0.0.0 should not be loopback (wildcard, handled separately)")
	}
}

// TestIsLoopbackHost_Localhost pins the fix resolving "localhost" instead of
// trusting the bare string: on any machine where the resolver is set up
// normally (the case in CI and virtually all deployment environments),
// "localhost" must still resolve to a loopback address and pass.
func TestIsLoopbackHost_Localhost(t *testing.T) {
	if !isLoopbackHost("localhost") {
		t.Error(`"localhost" should resolve to a loopback address in a normal environment`)
	}
}

func TestIsLoopbackHost_UnresolvableHostnameNotTrusted(t *testing.T) {
	// Only "localhost" gets the resolve-with-fallback treatment; any other
	// hostname (resolvable or not) is not a loopback address by this check.
	if isLoopbackHost("this-host-does-not-exist.invalid") {
		t.Error("an arbitrary unresolvable hostname must not be treated as loopback")
	}
}
