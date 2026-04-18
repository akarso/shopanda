package config

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadResult contains the loaded Config and metadata about the load process.
type LoadResult struct {
	Config     *Config
	DotEnvUsed bool   // true if a .env file was loaded
	DotEnvPath string // path to the .env file that was loaded (empty if none)
}

// Config holds all application configuration.
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Database  DatabaseConfig  `yaml:"database"`
	Log       LogConfig       `yaml:"log"`
	Auth      AuthConfig      `yaml:"auth"`
	Mail      MailConfig      `yaml:"mail"`
	Media     MediaConfig     `yaml:"media"`
	Cache     CacheConfig     `yaml:"cache"`
	Frontend  FrontendConfig  `yaml:"frontend"`
	CDN       CDNConfig       `yaml:"cdn"`
	Webhooks  WebhooksConfig  `yaml:"webhooks"`
	RateLimit RateLimitConfig `yaml:"rate_limit"`
	Payment   PaymentConfig   `yaml:"payment"`
}

// WebhooksConfig holds per-provider webhook secrets.
type WebhooksConfig struct {
	Secrets map[string]string `yaml:"secrets"`
}

// Secret returns the webhook secret for the given provider, or empty string.
func (w WebhooksConfig) Secret(provider string) string {
	return w.Secrets[provider]
}

// RateLimitConfig holds rate limiting settings.
type RateLimitConfig struct {
	Enabled        bool                 `yaml:"enabled"`
	Default        RateLimitRule        `yaml:"default"`
	PerRoute       []RouteRateLimitRule `yaml:"per_route"`
	TrustedProxies []string             `yaml:"trusted_proxies"`
}

// RateLimitRule defines a token-bucket rate: Rate tokens per second, Burst max.
type RateLimitRule struct {
	Rate  float64 `yaml:"rate"`
	Burst int     `yaml:"burst"`
}

// RouteRateLimitRule applies a rate limit rule to a specific path prefix.
type RouteRateLimitRule struct {
	PathPrefix string  `yaml:"path_prefix"`
	Rate       float64 `yaml:"rate"`
	Burst      int     `yaml:"burst"`
}

type ServerConfig struct {
	Host          string `yaml:"host"`
	Port          int    `yaml:"port"`
	PublicBaseURL string `yaml:"public_base_url"`
}

type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Name     string `yaml:"name"`
	SSLMode  string `yaml:"sslmode"`
}

func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		d.User, d.Password, d.Host, d.Port, d.Name, d.SSLMode,
	)
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type AuthConfig struct {
	JWTSecret string `yaml:"jwt_secret"`
	JWTTTL    string `yaml:"jwt_ttl"`
}

type MailConfig struct {
	Driver string     `yaml:"driver"`
	SMTP   SMTPConfig `yaml:"smtp"`
}

type SMTPConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	From     string `yaml:"from"`
}

type MediaConfig struct {
	Storage string             `yaml:"storage"`
	Local   LocalStorageConfig `yaml:"local"`
}

type LocalStorageConfig struct {
	BasePath string `yaml:"base_path"`
	BaseURL  string `yaml:"base_url"`
}

type CacheConfig struct {
	Driver string `yaml:"driver"`
}

type FrontendConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Mode      string `yaml:"mode"`
	ThemePath string `yaml:"theme_path"`
}

type CDNConfig struct {
	BaseURL string `yaml:"base_url"`
}

// PaymentConfig holds payment provider settings.
type PaymentConfig struct {
	Stripe StripeConfig `yaml:"stripe"`
}

// StripeConfig holds Stripe-specific settings.
type StripeConfig struct {
	Enabled   bool   `yaml:"enabled"`
	SecretKey string `yaml:"secret_key"`
}

// values holds flattened dot-notation keys for generic access.
var values map[string]string

// Load reads a YAML config file and overlays environment variables.
// It looks for a .env file beside the config file first, then falls back to
// the working directory. Variables already set in the OS environment take
// precedence over .env values.
//
// Precedence (highest → lowest):
//  1. OS environment variables (shell export, or any service manager)
//  2. .env file values (development fallback)
//  3. YAML config file values
//  4. Built-in defaults
func Load(path string) (*LoadResult, error) {
	var dotEnvUsed bool
	var dotEnvPath string

	// Try .env beside the config file first.
	configDirEnv := filepath.Join(filepath.Dir(path), ".env")
	used, err := loadDotEnv(configDirEnv)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if used {
		dotEnvUsed = true
		dotEnvPath = configDirEnv
	}

	// Fallback: try .env in the working directory when config is in a subdirectory.
	if !dotEnvUsed && configDirEnv != ".env" {
		used, err = loadDotEnv(".env")
		if err != nil {
			return nil, fmt.Errorf("config: %w", err)
		}
		if used {
			dotEnvUsed = true
			dotEnvPath = ".env"
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	cfg := defaults()

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}

	applyEnv(&cfg)

	if err := normalizePublicBaseURL(&cfg); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	values = flatten(&cfg)

	return &LoadResult{Config: &cfg, DotEnvUsed: dotEnvUsed, DotEnvPath: dotEnvPath}, nil
}

// Get returns a config value by dot-notation key, or empty string if not found.
func Get(key string) string {
	return values[key]
}

// GetOrDefault returns the value for key, or fallback if not found.
func GetOrDefault(key, fallback string) string {
	if v, ok := values[key]; ok && v != "" {
		return v
	}
	return fallback
}

// GetString is an alias for Get.
func GetString(key string) string {
	return Get(key)
}

// GetInt returns the value as int, or 0 if not found or not parseable.
func GetInt(key string) int {
	v, _ := strconv.Atoi(Get(key))
	return v
}

// GetFloat returns the value as float64, or 0 if not found or not parseable.
func GetFloat(key string) float64 {
	v, _ := strconv.ParseFloat(Get(key), 64)
	return v
}

func defaults() Config {
	return Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8080,
		},
		Database: DatabaseConfig{
			Host:    "localhost",
			Port:    5432,
			User:    "shopanda",
			Name:    "shopanda",
			SSLMode: "disable",
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
		Auth: AuthConfig{
			JWTSecret: "",
			JWTTTL:    "24h",
		},
		Mail: MailConfig{
			Driver: "smtp",
			SMTP: SMTPConfig{
				Host: "localhost",
				Port: 587,
				From: "noreply@localhost",
			},
		},
		Media: MediaConfig{
			Storage: "local",
			Local: LocalStorageConfig{
				BasePath: "./public/media",
				BaseURL:  "/media",
			},
		},
		Cache: CacheConfig{
			Driver: "postgres",
		},
		Frontend: FrontendConfig{
			Enabled:   false,
			Mode:      "ssr",
			ThemePath: "themes/default",
		},
	}
}

// normalizePublicBaseURL validates and normalizes the PublicBaseURL field.
// If empty, it falls back to http://host:port from the server config.
// If set, it defaults the scheme to https when missing, strips trailing slashes,
// and returns an error if the value is not a valid URL.
func normalizePublicBaseURL(cfg *Config) error {
	raw := cfg.Server.PublicBaseURL
	if raw == "" {
		host := cfg.Server.Host
		if host == "" || host == "0.0.0.0" || host == "::" {
			return fmt.Errorf("server.public_base_url: must be set explicitly when server.host is a wildcard bind address (%q)", host)
		}
		cfg.Server.PublicBaseURL = fmt.Sprintf("http://%s:%d", host, cfg.Server.Port)
		return nil
	}

	// Default scheme to https if missing.
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("server.public_base_url: invalid URL %q: %w", raw, err)
	}
	if u.Host == "" {
		return fmt.Errorf("server.public_base_url: missing host in %q", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("server.public_base_url: unsupported scheme %q", u.Scheme)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("server.public_base_url: must not contain query or fragment")
	}

	cfg.Server.PublicBaseURL = u.Scheme + "://" + u.Host + strings.TrimRight(u.Path, "/")
	return nil
}

// applyEnv overlays environment variables with SHOPANDA_ prefix.
func applyEnv(cfg *Config) {
	if v := os.Getenv("SHOPANDA_SERVER_HOST"); v != "" {
		cfg.Server.Host = v
	}
	if v := os.Getenv("SHOPANDA_SERVER_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Server.Port = p
		}
	}
	if v := os.Getenv("SHOPANDA_SERVER_PUBLIC_BASE_URL"); v != "" {
		cfg.Server.PublicBaseURL = v
	}
	if v := os.Getenv("SHOPANDA_DATABASE_HOST"); v != "" {
		cfg.Database.Host = v
	}
	if v := os.Getenv("SHOPANDA_DATABASE_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Database.Port = p
		}
	}
	if v := os.Getenv("SHOPANDA_DATABASE_USER"); v != "" {
		cfg.Database.User = v
	}
	if v := os.Getenv("SHOPANDA_DATABASE_PASSWORD"); v != "" {
		cfg.Database.Password = v
	}
	if v := os.Getenv("SHOPANDA_DATABASE_NAME"); v != "" {
		cfg.Database.Name = v
	}
	if v := os.Getenv("SHOPANDA_DATABASE_SSLMODE"); v != "" {
		cfg.Database.SSLMode = v
	}
	if v := os.Getenv("SHOPANDA_LOG_LEVEL"); v != "" {
		cfg.Log.Level = v
	}
	if v := os.Getenv("SHOPANDA_LOG_FORMAT"); v != "" {
		cfg.Log.Format = v
	}
	if v := os.Getenv("SHOPANDA_AUTH_JWT_SECRET"); v != "" {
		cfg.Auth.JWTSecret = v
	}
	if v := os.Getenv("SHOPANDA_AUTH_JWT_TTL"); v != "" {
		cfg.Auth.JWTTTL = v
	}
	if v := os.Getenv("SHOPANDA_MAIL_DRIVER"); v != "" {
		cfg.Mail.Driver = v
	}
	if v := os.Getenv("SHOPANDA_MAIL_SMTP_HOST"); v != "" {
		cfg.Mail.SMTP.Host = v
	}
	if v := os.Getenv("SHOPANDA_MAIL_SMTP_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Mail.SMTP.Port = p
		}
	}
	if v := os.Getenv("SHOPANDA_MAIL_SMTP_USER"); v != "" {
		cfg.Mail.SMTP.User = v
	}
	if v := os.Getenv("SHOPANDA_MAIL_SMTP_PASSWORD"); v != "" {
		cfg.Mail.SMTP.Password = v
	}
	if v := os.Getenv("SHOPANDA_MAIL_SMTP_FROM"); v != "" {
		cfg.Mail.SMTP.From = v
	}
	if v := os.Getenv("SHOPANDA_MEDIA_STORAGE"); v != "" {
		cfg.Media.Storage = v
	}
	if v := os.Getenv("SHOPANDA_MEDIA_LOCAL_BASE_PATH"); v != "" {
		cfg.Media.Local.BasePath = v
	}
	if v := os.Getenv("SHOPANDA_MEDIA_LOCAL_BASE_URL"); v != "" {
		cfg.Media.Local.BaseURL = v
	}
	if v := os.Getenv("SHOPANDA_CACHE_DRIVER"); v != "" {
		cfg.Cache.Driver = v
	}
	if v := os.Getenv("SHOPANDA_FRONTEND_ENABLED"); v != "" {
		cfg.Frontend.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("SHOPANDA_FRONTEND_MODE"); v != "" {
		cfg.Frontend.Mode = v
	}
	if v := os.Getenv("SHOPANDA_FRONTEND_THEME_PATH"); v != "" {
		cfg.Frontend.ThemePath = v
	}
	if v := os.Getenv("SHOPANDA_CDN_BASE_URL"); v != "" {
		cfg.CDN.BaseURL = v
	}
	if v := os.Getenv("SHOPANDA_PAYMENT_STRIPE_ENABLED"); v != "" {
		cfg.Payment.Stripe.Enabled, _ = strconv.ParseBool(v)
	}
	if v := os.Getenv("SHOPANDA_PAYMENT_STRIPE_SECRET_KEY"); v != "" {
		cfg.Payment.Stripe.SecretKey = v
	}
	// Webhook secrets: SHOPANDA_WEBHOOKS_SECRET_<PROVIDER>=<secret>
	const whPrefix = "SHOPANDA_WEBHOOKS_SECRET_"
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, whPrefix) {
			continue
		}
		kv := strings.SplitN(e, "=", 2)
		if len(kv) != 2 || kv[1] == "" {
			continue
		}
		provider := strings.ToLower(strings.TrimPrefix(kv[0], whPrefix))
		if provider == "" {
			continue
		}
		if cfg.Webhooks.Secrets == nil {
			cfg.Webhooks.Secrets = make(map[string]string)
		}
		cfg.Webhooks.Secrets[provider] = kv[1]
	}
	if v := os.Getenv("SHOPANDA_RATE_LIMIT_ENABLED"); v != "" {
		cfg.RateLimit.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("SHOPANDA_RATE_LIMIT_DEFAULT_RATE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			cfg.RateLimit.Default.Rate = f
		}
	}
	if v := os.Getenv("SHOPANDA_RATE_LIMIT_DEFAULT_BURST"); v != "" {
		if b, err := strconv.Atoi(v); err == nil && b > 0 {
			cfg.RateLimit.Default.Burst = b
		}
	}
}

// flatten converts the Config struct into a dot-notation key-value map.
func flatten(cfg *Config) map[string]string {
	m := make(map[string]string)
	m["server.host"] = cfg.Server.Host
	m["server.port"] = strconv.Itoa(cfg.Server.Port)
	m["server.public_base_url"] = cfg.Server.PublicBaseURL
	m["database.host"] = cfg.Database.Host
	m["database.port"] = strconv.Itoa(cfg.Database.Port)
	m["database.user"] = cfg.Database.User
	m["database.password"] = cfg.Database.Password
	m["database.name"] = cfg.Database.Name
	m["database.sslmode"] = cfg.Database.SSLMode
	m["log.level"] = cfg.Log.Level
	m["log.format"] = cfg.Log.Format
	m["auth.jwt_ttl"] = cfg.Auth.JWTTTL
	m["mail.driver"] = cfg.Mail.Driver
	m["mail.smtp.host"] = cfg.Mail.SMTP.Host
	m["mail.smtp.port"] = strconv.Itoa(cfg.Mail.SMTP.Port)
	m["mail.smtp.user"] = cfg.Mail.SMTP.User
	m["mail.smtp.from"] = cfg.Mail.SMTP.From
	m["media.storage"] = cfg.Media.Storage
	m["media.local.base_path"] = cfg.Media.Local.BasePath
	m["media.local.base_url"] = cfg.Media.Local.BaseURL
	m["cache.driver"] = cfg.Cache.Driver
	m["frontend.enabled"] = strconv.FormatBool(cfg.Frontend.Enabled)
	m["frontend.mode"] = cfg.Frontend.Mode
	m["frontend.theme_path"] = cfg.Frontend.ThemePath
	m["cdn.base_url"] = cfg.CDN.BaseURL
	m["payment.stripe.enabled"] = strconv.FormatBool(cfg.Payment.Stripe.Enabled)
	for k, v := range cfg.Webhooks.Secrets {
		m["webhooks.secrets."+k] = v
	}
	m["rate_limit.enabled"] = strconv.FormatBool(cfg.RateLimit.Enabled)
	m["rate_limit.default.rate"] = strconv.FormatFloat(cfg.RateLimit.Default.Rate, 'f', -1, 64)
	m["rate_limit.default.burst"] = strconv.Itoa(cfg.RateLimit.Default.Burst)
	return m
}

// DatabaseDSN is a convenience for building a DSN from env var or config.
// If DATABASE_URL env var is set, it takes precedence.
func DatabaseDSN(cfg *Config) string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return cfg.Database.DSN()
}

// FindConfigFile looks for config.yaml in common locations.
func FindConfigFile() string {
	candidates := []string{
		"configs/config.yaml",
		"config.yaml",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return candidates[0]
}

// String returns a redacted summary suitable for logging.
func (c *Config) String() string {
	password := "***"
	if c.Database.Password == "" {
		password = "(empty)"
	}
	return strings.Join([]string{
		fmt.Sprintf("server=%s:%d", c.Server.Host, c.Server.Port),
		fmt.Sprintf("database=%s@%s:%d/%s?sslmode=%s password=%s",
			c.Database.User, c.Database.Host, c.Database.Port,
			c.Database.Name, c.Database.SSLMode, password),
		fmt.Sprintf("log.level=%s log.format=%s", c.Log.Level, c.Log.Format),
		fmt.Sprintf("auth.jwt_ttl=%s", c.Auth.JWTTTL),
		fmt.Sprintf("media.storage=%s media.local.base_path=%s media.local.base_url=%s", c.Media.Storage, c.Media.Local.BasePath, c.Media.Local.BaseURL),
		fmt.Sprintf("cache.driver=%s", c.Cache.Driver),
		fmt.Sprintf("frontend.enabled=%t frontend.mode=%s frontend.theme_path=%s", c.Frontend.Enabled, c.Frontend.Mode, c.Frontend.ThemePath),
	}, " ")
}

// loadDotEnv reads a .env file and sets variables that are NOT already present
// in the OS environment. This ensures that explicitly set environment variables
// always take precedence over .env values.
//
// Returns (true, nil) if the file was found and loaded, (false, nil) if the
// file does not exist, or (false, err) for any other I/O failure.
func loadDotEnv(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip blank lines and comments.
		if line == "" || line[0] == '#' {
			continue
		}

		key, val, ok := parseDotEnvLine(line)
		if !ok {
			continue
		}

		// Only set if not already in the OS environment.
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, val); err != nil {
				return false, fmt.Errorf("setenv %s: %w", key, err)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	return true, nil
}

// parseDotEnvLine parses a single "KEY=VALUE" line from a .env file.
// Supports bare values, double- or single-quoted values, the "export" prefix,
// and empty values (KEY=). This is intentionally NOT a full dotenv parser:
//   - Inline comments are NOT stripped (KEY=value # comment keeps "value # comment")
//   - Escape sequences inside quotes are NOT processed (literal \n stays as \n)
//   - Only the outermost matching quote pair is stripped
//
// Returns key, value, ok. Returns ok=false for comments, blank lines, or
// malformed input (missing '=' or empty key).
func parseDotEnvLine(line string) (string, string, bool) {
	idx := strings.IndexByte(line, '=')
	if idx <= 0 {
		return "", "", false
	}

	key := strings.TrimSpace(line[:idx])
	val := strings.TrimSpace(line[idx+1:])

	// Strip optional "export " prefix.
	key = strings.TrimPrefix(key, "export ")
	key = strings.TrimSpace(key)

	if key == "" {
		return "", "", false
	}

	// Strip matching quotes from value.
	if len(val) >= 2 {
		if (val[0] == '"' && val[len(val)-1] == '"') ||
			(val[0] == '\'' && val[len(val)-1] == '\'') {
			val = val[1 : len(val)-1]
		}
	}

	return key, val, true
}
