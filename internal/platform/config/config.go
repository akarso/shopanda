package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds all application configuration.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Log      LogConfig      `yaml:"log"`
	Auth     AuthConfig     `yaml:"auth"`
	Mail     MailConfig     `yaml:"mail"`
	Media    MediaConfig    `yaml:"media"`
	Cache    CacheConfig    `yaml:"cache"`
	Frontend FrontendConfig `yaml:"frontend"`
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

// values holds flattened dot-notation keys for generic access.
var values map[string]string

// Load reads a YAML config file and overlays environment variables.
// Env vars use the prefix SHOPANDA_ and replace dots/nesting with underscores.
// Example: server.port -> SHOPANDA_SERVER_PORT
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	cfg := defaults()

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}

	applyEnv(&cfg)
	values = flatten(&cfg)

	return &cfg, nil
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
