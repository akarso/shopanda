package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	path := writeYAML(t, "")

	cfg, err := Load(path)
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
	yaml := `
server:
  port: 9090
log:
  level: debug
`
	path := writeYAML(t, yaml)

	cfg, err := Load(path)
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
	yaml := `
server:
  port: 9090
database:
  host: yamlhost
`
	path := writeYAML(t, yaml)

	t.Setenv("SHOPANDA_SERVER_PORT", "7070")
	t.Setenv("SHOPANDA_DATABASE_HOST", "envhost")

	cfg, err := Load(path)
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

	_, err := Load(path)
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
	yaml := `
server:
  port: 3000
`
	path := writeYAML(t, yaml)

	_, err := Load(path)
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
	yaml := `
server:
  port: 5000
`
	path := writeYAML(t, yaml)

	_, err := Load(path)
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

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	want := "postgres://shopanda:secret@localhost:5432/shopanda?sslmode=disable"
	if got := cfg.Database.DSN(); got != want {
		t.Errorf("DSN() = %q, want %q", got, want)
	}
}

func TestDatabaseDSN_EnvOverride(t *testing.T) {
	path := writeYAML(t, "")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	t.Setenv("DATABASE_URL", "postgres://override:5433/other")

	got := DatabaseDSN(cfg)
	if got != "postgres://override:5433/other" {
		t.Errorf("DatabaseDSN() = %q, want env override", got)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Error("Load() expected error for missing file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	path := writeYAML(t, "{{invalid yaml")

	_, err := Load(path)
	if err == nil {
		t.Error("Load() expected error for invalid YAML")
	}
}

func TestConfigString_RedactsPassword(t *testing.T) {
	yaml := `
database:
  password: supersecret
`
	path := writeYAML(t, yaml)

	cfg, err := Load(path)
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
