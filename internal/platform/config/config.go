package config

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"net"
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
	Server      ServerConfig      `yaml:"server"`
	HTTP        HTTPConfig        `yaml:"http"`
	Database    DatabaseConfig    `yaml:"database"`
	Log         LogConfig         `yaml:"log"`
	Auth        AuthConfig        `yaml:"auth"`
	Mail        MailConfig        `yaml:"mail"`
	Media       MediaConfig       `yaml:"media"`
	Cache       CacheConfig       `yaml:"cache"`
	Queue       QueueConfig       `yaml:"queue"`
	Plugins     PluginsConfig     `yaml:"plugins"`
	Frontend    FrontendConfig    `yaml:"frontend"`
	CDN         CDNConfig         `yaml:"cdn"`
	Webhooks    WebhooksConfig    `yaml:"webhooks"`
	RateLimit   RateLimitConfig   `yaml:"rate_limit"`
	Payment     PaymentConfig     `yaml:"payment"`
	Search      SearchConfig      `yaml:"search"`
	Metrics     MetricsConfig     `yaml:"metrics"`
	Tracing     TracingConfig     `yaml:"tracing"`
	Dev         DevConfig         `yaml:"dev"`
	StoreCredit StoreCreditConfig `yaml:"store_credit"`
}

// StoreCreditConfig bounds how much store credit an admin can issue in a
// single request. MaxIssueAmount is in the currency's minor units (e.g.
// cents), matching shared.Money. Zero disables the cap — an explicit
// operator opt-out, not an oversight.
type StoreCreditConfig struct {
	MaxIssueAmount int64 `yaml:"max_issue_amount"`
}

// TracingConfig holds optional OpenTelemetry trace export settings.
// Disabled by default — when Enabled, spans for HTTP requests and the
// checkout workflow are exported via OTLP/HTTP to Endpoint. Unlike Metrics,
// there is no separate listener: this is an outbound exporter, not a
// server.
type TracingConfig struct {
	Enabled bool `yaml:"enabled"`
	// Endpoint is the OTLP/HTTP collector address as host:port (e.g.
	// "localhost:4318" for a local collector, or a cloud vendor's OTLP
	// ingest host) — no scheme, matching otlptracehttp.WithEndpoint.
	Endpoint string `yaml:"endpoint"`
	// Insecure disables TLS for the exporter connection — for a local/
	// same-host collector only; never set true against a remote endpoint.
	Insecure bool `yaml:"insecure"`
	// SampleRatio is the fraction of traces to sample, 0.0–1.0. Left
	// unmentioned in YAML, it defaults to 1.0 (sample everything) — trace
	// volume is naturally bounded by request volume, so unlike a
	// public-facing choice this is safe to default high; operators scale
	// it down under load. An explicit 0 is a real "record spans but export
	// none" signal, not treated as unset — see DefaultTracingSampleRatio.
	SampleRatio float64 `yaml:"sample_ratio"`
	// Headers are sent with every OTLP export request — e.g. an API key
	// header some SaaS collectors (Grafana Cloud, Honeycomb) require.
	Headers map[string]string `yaml:"headers"`
}

// MetricsConfig holds optional Prometheus metrics settings. Disabled by
// default — when Enabled, a dedicated /metrics listener binds to Listen
// (default loopback-only) so scrapes are never publicly exposed unless an
// operator explicitly rebinds Listen onto a private scrape network.
type MetricsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Listen  string `yaml:"listen"`
}

// DevConfig holds development-mode runtime options.
type DevConfig struct {
	EmbedScheduler bool `yaml:"embed_scheduler"`
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

// HTTPConfig holds HTTP boundary settings (body limits, etc.).
type HTTPConfig struct {
	// MaxBodyBytes is the default request body cap for non-media routes (bytes).
	MaxBodyBytes int64 `yaml:"max_body_bytes"`
	// MediaMaxBodyBytes is the body cap for admin media upload routes (bytes).
	MediaMaxBodyBytes int64 `yaml:"media_max_body_bytes"`
}

const (
	DefaultHTTPMaxBodyBytes      int64 = 1 << 20  // 1 MiB
	DefaultHTTPMediaMaxBodyBytes int64 = 10 << 20 // 10 MiB
)

// DefaultStoreCreditMaxIssueAmount caps a single admin store-credit issuance
// at 100000 minor units (e.g. $1,000.00) absent an explicit operator
// override — a conservative default against a fat-fingered or compromised
// admin session minting an unbounded amount in one request.
const DefaultStoreCreditMaxIssueAmount int64 = 100000

// DefaultTracingSampleRatio samples every trace absent an explicit
// operator override. Seeded in defaults() rather than applied as a
// post-parse fallback so an explicit tracing.sample_ratio: 0 in YAML
// (yaml.Unmarshal overwrites this default) is distinguishable from the
// operator never mentioning the field at all.
const DefaultTracingSampleRatio = 1.0

// DefaultMetricsListen binds /metrics to loopback only, so enabling metrics
// never exposes them beyond the host unless an operator explicitly changes
// metrics.listen to a private scrape network address.
const DefaultMetricsListen = "127.0.0.1:9090"

// DefaultWorkerMetricsListen is the port the worker process binds /metrics
// to when metrics.listen was left at DefaultMetricsListen — serve and
// worker often run as separate processes on the same host, and both
// defaulting to the same port would fail one of them at startup with
// "address already in use". Only applied when the operator hasn't
// explicitly overridden metrics.listen (see cmd/api's runWorker).
const DefaultWorkerMetricsListen = "127.0.0.1:9091"

type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Name     string `yaml:"name"`
	SSLMode  string `yaml:"sslmode"`
}

func (d DatabaseConfig) DSN() string {
	query := url.Values{}
	if d.SSLMode != "" {
		query.Set("sslmode", d.SSLMode)
	}

	u := &url.URL{
		Scheme:   "postgres",
		Host:     net.JoinHostPort(d.Host, strconv.Itoa(d.Port)),
		Path:     "/" + d.Name,
		RawQuery: query.Encode(),
	}
	if d.User != "" {
		if d.Password != "" {
			u.User = url.UserPassword(d.User, d.Password)
		} else {
			u.User = url.User(d.User)
		}
	}
	return u.String()
}

func normalizeDatabaseURL(raw string) string {
	if raw == "" {
		return ""
	}

	_, err := url.Parse(raw)
	if err == nil {
		return raw
	}

	repaired, repairErr := repairDatabaseURLUserinfo(raw)
	if repairErr != nil {
		return raw
	}
	return repaired
}

func repairDatabaseURLUserinfo(raw string) (string, error) {
	scheme, remainder, ok := strings.Cut(raw, "://")
	if !ok {
		return "", fmt.Errorf("missing scheme")
	}

	authority := remainder
	suffix := ""
	if idx := strings.IndexAny(remainder, "/?#"); idx >= 0 {
		authority = remainder[:idx]
		suffix = remainder[idx:]
	}

	at := strings.LastIndex(authority, "@")
	if at < 0 {
		return "", fmt.Errorf("missing userinfo")
	}

	userinfo := authority[:at]
	host := authority[at+1:]
	if host == "" {
		return "", fmt.Errorf("missing host")
	}

	user := userinfo
	password := ""
	if colon := strings.Index(userinfo, ":"); colon >= 0 {
		user = userinfo[:colon]
		password = userinfo[colon+1:]
	}

	repaired := scheme + "://" + escapeDatabaseURLUserinfo(user)
	if password != "" || strings.Contains(userinfo, ":") {
		repaired += ":" + escapeDatabaseURLUserinfo(password)
	}
	repaired += "@" + host + suffix

	parsed, err := url.Parse(repaired)
	if err != nil {
		return "", err
	}
	return parsed.String(), nil
}

func escapeDatabaseURLUserinfo(value string) string {
	if value == "" {
		return ""
	}

	var builder strings.Builder
	for i := 0; i < len(value); i++ {
		b := value[i]
		if isUnreservedUserinfoByte(b) {
			builder.WriteByte(b)
			continue
		}
		if b == '%' && i+2 < len(value) && isHexByte(value[i+1]) && isHexByte(value[i+2]) {
			builder.WriteByte('%')
			builder.WriteByte(value[i+1])
			builder.WriteByte(value[i+2])
			i += 2
			continue
		}
		builder.WriteString(fmt.Sprintf("%%%02X", b))
	}
	return builder.String()
}

func isUnreservedUserinfoByte(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '-' || b == '.' || b == '_' || b == '~'
}

func isHexByte(b byte) bool {
	return (b >= '0' && b <= '9') ||
		(b >= 'a' && b <= 'f') ||
		(b >= 'A' && b <= 'F')
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type AuthConfig struct {
	JWTSecret  string            `yaml:"jwt_secret"`
	JWTTTL     string            `yaml:"jwt_ttl"`
	MFAEnabled bool              `yaml:"mfa_enabled"`
	Lockout    AuthLockoutConfig `yaml:"lockout"`
}

// AuthLockoutConfig throttles failed password logins (IP + account key).
// store=cache uses the configured cache driver (postgres/redis) — preferred for multi-instance.
// store=memory is single-instance only (bounded in-process map).
//
// Window is a sliding TTL: each failed attempt refreshes the full duration, so
// continued failures can extend lockout beyond the original window.
type AuthLockoutConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Store       string `yaml:"store"` // cache | memory
	MaxFailures int    `yaml:"max_failures"`
	Window      string `yaml:"window"` // Go duration, e.g. "15m"
}

// Default auth.lockout values (shared by defaults() and normalizeAuthLockout).
const (
	DefaultAuthLockoutStore       = "cache"
	DefaultAuthLockoutMaxFailures = 10
	DefaultAuthLockoutWindow      = "15m"
)

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
	Storage    string                     `yaml:"storage"`
	Local      LocalStorageConfig         `yaml:"local"`
	S3         S3StorageConfig            `yaml:"s3"`
	Thumbnails map[string]ThumbnailConfig `yaml:"thumbnails"`
	WebP       WebPConfig                 `yaml:"webp"`
}

// WebPConfig controls automatic WebP conversion of thumbnails.
type WebPConfig struct {
	Enabled bool `yaml:"enabled"`
	Quality int  `yaml:"quality"`
}

// ThumbnailConfig defines a named thumbnail preset.
type ThumbnailConfig struct {
	Width  int    `yaml:"width"`
	Height int    `yaml:"height"`
	Fit    string `yaml:"fit"`
}

type LocalStorageConfig struct {
	BasePath string `yaml:"base_path"`
	BaseURL  string `yaml:"base_url"`
}

// S3StorageConfig holds settings for S3-compatible storage backends.
type S3StorageConfig struct {
	Endpoint  string `yaml:"endpoint"`
	Bucket    string `yaml:"bucket"`
	Region    string `yaml:"region"`
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
	BaseURL   string `yaml:"base_url"`
	PublicACL bool   `yaml:"public_acl"`
}

// SearchConfig holds product search settings.
type SearchConfig struct {
	Engine      string            `yaml:"engine"`
	Meilisearch MeilisearchConfig `yaml:"meilisearch"`
}

// MeilisearchConfig holds Meilisearch connection settings.
type MeilisearchConfig struct {
	Host   string `yaml:"host"`
	APIKey string `yaml:"api_key"`
	Index  string `yaml:"index"`
}

type CacheConfig struct {
	Driver string           `yaml:"driver"`
	Redis  RedisCacheConfig `yaml:"redis"`
}

// RedisCacheConfig holds Redis cache connection settings.
type RedisCacheConfig struct {
	URL       string `yaml:"url"`
	KeyPrefix string `yaml:"key_prefix"`
}

// QueueConfig holds background job queue settings.
type QueueConfig struct {
	Driver   string              `yaml:"driver"`
	Redis    RedisQueueConfig    `yaml:"redis"`
	RabbitMQ RabbitMQQueueConfig `yaml:"rabbitmq"`
	Kafka    KafkaQueueConfig    `yaml:"kafka"`
	SQS      SQSQueueConfig      `yaml:"sqs"`
}

// KafkaQueueConfig holds Kafka job queue connection settings.
type KafkaQueueConfig struct {
	Brokers     []string `yaml:"brokers"`
	TopicPrefix string   `yaml:"topic_prefix"`
}

// SQSQueueConfig holds Amazon SQS job queue connection settings.
type SQSQueueConfig struct {
	QueueURL       string `yaml:"queue_url"`
	FailedQueueURL string `yaml:"failed_queue_url"`
	Region         string `yaml:"region"`
}

// RabbitMQQueueConfig holds RabbitMQ job queue connection settings.
type RabbitMQQueueConfig struct {
	URL         string `yaml:"url"`
	QueuePrefix string `yaml:"queue_prefix"`
}

// RedisQueueConfig holds Redis job queue connection settings.
type RedisQueueConfig struct {
	URL       string `yaml:"url"`
	KeyPrefix string `yaml:"key_prefix"`
}

// PluginsConfig holds plugin system settings.
type PluginsConfig struct {
	DependsOn       map[string][]string         `yaml:"depends_on"`
	Core            CorePluginsConfig           `yaml:"core"`
	GraphQL         GraphQLPluginConfig         `yaml:"graphql"`
	Example         ExamplePluginConfig         `yaml:"example"`
	SlotsDemo       SlotsDemoPluginConfig       `yaml:"slotsdemo"`
	CartDemo        CartDemoPluginConfig        `yaml:"cartdemo"`
	TaxDemo         TaxDemoPluginConfig         `yaml:"taxdemo"`
	MailDemo        MailDemoPluginConfig        `yaml:"maildemo"`
	PromoDemo       PromoDemoPluginConfig       `yaml:"promodemo"`
	ImportDemo      ImportDemoPluginConfig      `yaml:"importdemo"`
	ExportDemo      ExportDemoPluginConfig      `yaml:"exportdemo"`
	CheckoutDemo    CheckoutDemoPluginConfig    `yaml:"checkoutdemo"`
	IntegrationDemo IntegrationDemoPluginConfig `yaml:"integrationdemo"`
	WarehouseDemo   WarehouseDemoPluginConfig   `yaml:"warehousedemo"`
	PimDemo         PimDemoPluginConfig         `yaml:"pimdemo"`
	B2B             B2BPluginConfig             `yaml:"b2b"`
}

// GraphQLPluginConfig toggles the optional read-only GraphQL API core plugin.
type GraphQLPluginConfig struct {
	Enabled bool `yaml:"enabled"`
}

// ExamplePluginConfig toggles the reference external plugin in plugins/example.
type ExamplePluginConfig struct {
	Enabled       bool  `yaml:"enabled"`
	FeeMinorUnits int64 `yaml:"fee_minor_units"`
}

// SlotsDemoPluginConfig toggles the slots reference plugin in plugins/slotsdemo.
type SlotsDemoPluginConfig struct {
	Enabled bool `yaml:"enabled"`
}

// CartDemoPluginConfig toggles the cart rules reference plugin in plugins/cartdemo.
type CartDemoPluginConfig struct {
	Enabled               bool  `yaml:"enabled"`
	MinQuantity           int   `yaml:"min_quantity"`
	HandlingFeeMinorUnits int64 `yaml:"handling_fee_minor_units"`
}

// TaxDemoPluginConfig toggles the tax port replacement reference plugin in plugins/taxdemo.
type TaxDemoPluginConfig struct {
	Enabled     bool `yaml:"enabled"`
	FlatRateBPS int  `yaml:"flat_rate_bps"`
}

// MailDemoPluginConfig toggles the mail sender port replacement reference plugin in plugins/maildemo.
type MailDemoPluginConfig struct {
	Enabled       bool   `yaml:"enabled"`
	SubjectPrefix string `yaml:"subject_prefix"`
}

// PromoDemoPluginConfig toggles the promotion rule evaluator reference plugin in plugins/promodemo.
type PromoDemoPluginConfig struct {
	Enabled bool `yaml:"enabled"`
}

// ImportDemoPluginConfig toggles the CSV import remap reference plugin in plugins/importdemo.
type ImportDemoPluginConfig struct {
	Enabled bool `yaml:"enabled"`
}

// ExportDemoPluginConfig toggles the CSV export remap reference plugin in plugins/exportdemo.
type ExportDemoPluginConfig struct {
	Enabled bool `yaml:"enabled"`
}

// CheckoutDemoPluginConfig toggles the positioned checkout validation reference plugin.
type CheckoutDemoPluginConfig struct {
	Enabled bool `yaml:"enabled"`
}

// IntegrationDemoPluginConfig toggles the inbound ERP order-status reference plugin.
type IntegrationDemoPluginConfig struct {
	Enabled               bool   `yaml:"enabled"`
	IntegrationAPIKey     string `yaml:"integration_api_key"`
	IntegrationHMACSecret string `yaml:"integration_hmac_secret"`
}

// WarehouseDemoPluginConfig toggles the outbound warehouse stock reference plugin.
type WarehouseDemoPluginConfig struct {
	Enabled          bool   `yaml:"enabled"`
	WarehouseBaseURL string `yaml:"warehouse_base_url"`
	WarehouseAPIKey  string `yaml:"warehouse_api_key"`
	SyncCron         string `yaml:"sync_cron"`
}

// PimDemoPluginConfig toggles the outbound PIM GraphQL PDP enrichment reference plugin.
type PimDemoPluginConfig struct {
	Enabled            bool   `yaml:"enabled"`
	PimGraphQLEndpoint string `yaml:"pim_graphql_endpoint"`
	PimAPIKey          string `yaml:"pim_api_key"`
	CacheTTL           string `yaml:"cache_ttl"`
}

// B2BPluginConfig toggles the commercial B2B module in plugins/b2b.
type B2BPluginConfig struct {
	Enabled    bool   `yaml:"enabled"`
	LicenseKey string `yaml:"license_key"`
}

// CorePluginsConfig allows explicit enable/disable of core infrastructure plugins.
// When a field is nil, enablement is inferred from the corresponding driver switch
// (search.engine, cache.driver, queue.driver).
type CorePluginsConfig struct {
	PostgresSearch *bool `yaml:"postgres_search,omitempty"`
	PostgresCache  *bool `yaml:"postgres_cache,omitempty"`
	PostgresQueue  *bool `yaml:"postgres_queue,omitempty"`
}

type FrontendConfig struct {
	Enabled           bool   `yaml:"enabled"`
	Mode              string `yaml:"mode"`
	ThemePath         string `yaml:"theme_path"`
	CSPEnabled        bool   `yaml:"csp_enabled"`
	StrictSlotMarkers bool   `yaml:"strict_slot_markers"`
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
	Enabled       bool   `yaml:"enabled"`
	SecretKey     string `yaml:"secret_key"`
	WebhookSecret string `yaml:"webhook_secret"`
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

	if err := normalizeAndValidate(&cfg); err != nil {
		return nil, err
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
		HTTP: HTTPConfig{
			MaxBodyBytes:      DefaultHTTPMaxBodyBytes,
			MediaMaxBodyBytes: DefaultHTTPMediaMaxBodyBytes,
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
			Lockout: AuthLockoutConfig{
				Enabled:     true,
				Store:       DefaultAuthLockoutStore,
				MaxFailures: DefaultAuthLockoutMaxFailures,
				Window:      DefaultAuthLockoutWindow,
			},
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
			Thumbnails: map[string]ThumbnailConfig{
				"small":  {Width: 150, Height: 150, Fit: "cover"},
				"medium": {Width: 400, Height: 400, Fit: "contain"},
				"large":  {Width: 800, Height: 800, Fit: "contain"},
			},
			WebP: WebPConfig{Enabled: false, Quality: 80},
		},
		Search: SearchConfig{
			Engine: "postgres",
			Meilisearch: MeilisearchConfig{
				Host:  "http://localhost:7700",
				Index: "products",
			},
		},
		Cache: CacheConfig{
			Driver: "postgres",
		},
		Queue: QueueConfig{
			Driver: "postgres",
		},
		Frontend: FrontendConfig{
			Enabled:   false,
			Mode:      "ssr",
			ThemePath: "themes/default",
		},
		Dev: DevConfig{
			EmbedScheduler: true,
		},
		RateLimit: RateLimitConfig{
			Enabled: true,
			Default: RateLimitRule{Rate: 10, Burst: 20},
		},
		Metrics: MetricsConfig{
			Enabled: false,
			Listen:  DefaultMetricsListen,
		},
		Tracing: TracingConfig{
			Enabled: false,
			// Seeded here (not applied as a post-parse fallback) so YAML's
			// zero-value semantics distinguish "operator never mentioned
			// sample_ratio" (this default survives) from "operator wrote
			// sample_ratio: 0" (yaml.Unmarshal overwrites it to 0, a real
			// disable-sampling signal, not a value to helpfully "fix").
			SampleRatio: DefaultTracingSampleRatio,
		},
		StoreCredit: StoreCreditConfig{
			MaxIssueAmount: DefaultStoreCreditMaxIssueAmount,
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
	if v := os.Getenv("SHOPANDA_HTTP_MAX_BODY_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			cfg.HTTP.MaxBodyBytes = n
		}
	}
	if v := os.Getenv("SHOPANDA_HTTP_MEDIA_MAX_BODY_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			cfg.HTTP.MediaMaxBodyBytes = n
		}
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
	if v := os.Getenv("SHOPANDA_AUTH_MFA_ENABLED"); v != "" {
		cfg.Auth.MFAEnabled = strings.EqualFold(v, "true") || v == "1"
	}
	if v := os.Getenv("SHOPANDA_AUTH_LOCKOUT_ENABLED"); v != "" {
		cfg.Auth.Lockout.Enabled = strings.EqualFold(v, "true") || v == "1"
	}
	if v := os.Getenv("SHOPANDA_AUTH_LOCKOUT_STORE"); v != "" {
		cfg.Auth.Lockout.Store = v
	}
	if v := os.Getenv("SHOPANDA_AUTH_LOCKOUT_MAX_FAILURES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Auth.Lockout.MaxFailures = n
		}
	}
	if v := os.Getenv("SHOPANDA_AUTH_LOCKOUT_WINDOW"); v != "" {
		cfg.Auth.Lockout.Window = v
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
	if v := os.Getenv("SHOPANDA_MEDIA_S3_ENDPOINT"); v != "" {
		cfg.Media.S3.Endpoint = v
	}
	if v := os.Getenv("SHOPANDA_MEDIA_S3_BUCKET"); v != "" {
		cfg.Media.S3.Bucket = v
	}
	if v := os.Getenv("SHOPANDA_MEDIA_S3_REGION"); v != "" {
		cfg.Media.S3.Region = v
	}
	if v := os.Getenv("SHOPANDA_MEDIA_S3_ACCESS_KEY"); v != "" {
		cfg.Media.S3.AccessKey = v
	}
	if v := os.Getenv("SHOPANDA_MEDIA_S3_SECRET_KEY"); v != "" {
		cfg.Media.S3.SecretKey = v
	}
	if v := os.Getenv("SHOPANDA_MEDIA_S3_BASE_URL"); v != "" {
		cfg.Media.S3.BaseURL = v
	}
	if v := os.Getenv("SHOPANDA_MEDIA_S3_PUBLIC_ACL"); v != "" {
		cfg.Media.S3.PublicACL = v == "true" || v == "1"
	}
	if v := os.Getenv("SHOPANDA_SEARCH_ENGINE"); v != "" {
		cfg.Search.Engine = v
	}
	if v := os.Getenv("SHOPANDA_SEARCH_MEILI_HOST"); v != "" {
		cfg.Search.Meilisearch.Host = v
	}
	if v := os.Getenv("SHOPANDA_SEARCH_MEILI_KEY"); v != "" {
		cfg.Search.Meilisearch.APIKey = v
	}
	if v := os.Getenv("SHOPANDA_SEARCH_MEILI_INDEX"); v != "" {
		cfg.Search.Meilisearch.Index = v
	}
	if v := os.Getenv("SHOPANDA_CACHE_DRIVER"); v != "" {
		cfg.Cache.Driver = v
	}
	if v := os.Getenv("SHOPANDA_CACHE_REDIS_URL"); v != "" {
		cfg.Cache.Redis.URL = v
	}
	if v := os.Getenv("SHOPANDA_CACHE_REDIS_KEY_PREFIX"); v != "" {
		cfg.Cache.Redis.KeyPrefix = v
	}
	if v := os.Getenv("SHOPANDA_QUEUE_DRIVER"); v != "" {
		cfg.Queue.Driver = v
	}
	if v := os.Getenv("REDIS_URL"); v != "" {
		if cfg.Cache.Redis.URL == "" {
			cfg.Cache.Redis.URL = v
		}
		if cfg.Queue.Redis.URL == "" {
			cfg.Queue.Redis.URL = v
		}
	}
	if v := os.Getenv("SHOPANDA_QUEUE_REDIS_URL"); v != "" {
		cfg.Queue.Redis.URL = v
	}
	if v := os.Getenv("SHOPANDA_QUEUE_REDIS_KEY_PREFIX"); v != "" {
		cfg.Queue.Redis.KeyPrefix = v
	}
	if v := os.Getenv("RABBITMQ_URL"); v != "" && cfg.Queue.RabbitMQ.URL == "" {
		cfg.Queue.RabbitMQ.URL = v
	}
	if v := os.Getenv("SHOPANDA_QUEUE_RABBITMQ_URL"); v != "" {
		cfg.Queue.RabbitMQ.URL = v
	}
	if v := os.Getenv("SHOPANDA_QUEUE_RABBITMQ_QUEUE_PREFIX"); v != "" {
		cfg.Queue.RabbitMQ.QueuePrefix = v
	}
	if v := os.Getenv("KAFKA_BROKERS"); v != "" && len(cfg.Queue.Kafka.Brokers) == 0 {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				cfg.Queue.Kafka.Brokers = append(cfg.Queue.Kafka.Brokers, part)
			}
		}
	}
	if v := os.Getenv("SHOPANDA_QUEUE_KAFKA_BROKERS"); v != "" {
		cfg.Queue.Kafka.Brokers = nil
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				cfg.Queue.Kafka.Brokers = append(cfg.Queue.Kafka.Brokers, part)
			}
		}
	}
	if v := os.Getenv("SHOPANDA_QUEUE_KAFKA_TOPIC_PREFIX"); v != "" {
		cfg.Queue.Kafka.TopicPrefix = v
	}
	if v := os.Getenv("SQS_QUEUE_URL"); v != "" && cfg.Queue.SQS.QueueURL == "" {
		cfg.Queue.SQS.QueueURL = v
	}
	if v := os.Getenv("SHOPANDA_QUEUE_SQS_QUEUE_URL"); v != "" {
		cfg.Queue.SQS.QueueURL = v
	}
	if v := os.Getenv("SQS_FAILED_QUEUE_URL"); v != "" && cfg.Queue.SQS.FailedQueueURL == "" {
		cfg.Queue.SQS.FailedQueueURL = v
	}
	if v := os.Getenv("SHOPANDA_QUEUE_SQS_FAILED_QUEUE_URL"); v != "" {
		cfg.Queue.SQS.FailedQueueURL = v
	}
	if v := os.Getenv("SHOPANDA_QUEUE_SQS_REGION"); v != "" {
		cfg.Queue.SQS.Region = v
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_GRAPHQL_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Plugins.GraphQL.Enabled = b
		}
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_EXAMPLE_ENABLED"); v != "" {
		cfg.Plugins.Example.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_EXAMPLE_FEE_MINOR_UNITS"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.Plugins.Example.FeeMinorUnits = n
		}
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_SLOTSDEMO_ENABLED"); v != "" {
		cfg.Plugins.SlotsDemo.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_CARTDEMO_ENABLED"); v != "" {
		cfg.Plugins.CartDemo.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_CARTDEMO_MIN_QUANTITY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Plugins.CartDemo.MinQuantity = n
		}
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_CARTDEMO_HANDLING_FEE_MINOR_UNITS"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.Plugins.CartDemo.HandlingFeeMinorUnits = n
		}
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_TAXDEMO_ENABLED"); v != "" {
		cfg.Plugins.TaxDemo.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_TAXDEMO_FLAT_RATE_BPS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Plugins.TaxDemo.FlatRateBPS = n
		}
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_MAILDEMO_ENABLED"); v != "" {
		cfg.Plugins.MailDemo.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_MAILDEMO_SUBJECT_PREFIX"); v != "" {
		cfg.Plugins.MailDemo.SubjectPrefix = v
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_PROMODEMO_ENABLED"); v != "" {
		cfg.Plugins.PromoDemo.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_IMPORTDEMO_ENABLED"); v != "" {
		cfg.Plugins.ImportDemo.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_EXPORTDEMO_ENABLED"); v != "" {
		cfg.Plugins.ExportDemo.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_CHECKOUTDEMO_ENABLED"); v != "" {
		cfg.Plugins.CheckoutDemo.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_INTEGRATIONDEMO_ENABLED"); v != "" {
		cfg.Plugins.IntegrationDemo.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_INTEGRATIONDEMO_INTEGRATION_API_KEY"); v != "" {
		cfg.Plugins.IntegrationDemo.IntegrationAPIKey = v
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_INTEGRATIONDEMO_INTEGRATION_HMAC_SECRET"); v != "" {
		cfg.Plugins.IntegrationDemo.IntegrationHMACSecret = v
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_WAREHOUSEDEMO_ENABLED"); v != "" {
		cfg.Plugins.WarehouseDemo.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_WAREHOUSEDEMO_WAREHOUSE_BASE_URL"); v != "" {
		cfg.Plugins.WarehouseDemo.WarehouseBaseURL = v
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_WAREHOUSEDEMO_WAREHOUSE_API_KEY"); v != "" {
		cfg.Plugins.WarehouseDemo.WarehouseAPIKey = v
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_WAREHOUSEDEMO_SYNC_CRON"); v != "" {
		cfg.Plugins.WarehouseDemo.SyncCron = v
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_PIMDEMO_ENABLED"); v != "" {
		cfg.Plugins.PimDemo.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_PIMDEMO_PIM_GRAPHQL_ENDPOINT"); v != "" {
		cfg.Plugins.PimDemo.PimGraphQLEndpoint = v
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_PIMDEMO_PIM_API_KEY"); v != "" {
		cfg.Plugins.PimDemo.PimAPIKey = v
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_PIMDEMO_CACHE_TTL"); v != "" {
		cfg.Plugins.PimDemo.CacheTTL = v
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_B2B_ENABLED"); v != "" {
		cfg.Plugins.B2B.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("SHOPANDA_PLUGINS_B2B_LICENSE_KEY"); v != "" {
		cfg.Plugins.B2B.LicenseKey = v
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
	if v := os.Getenv("SHOPANDA_FRONTEND_CSP_ENABLED"); v != "" {
		cfg.Frontend.CSPEnabled = v == "true" || v == "1"
	}
	if v := os.Getenv("SHOPANDA_FRONTEND_STRICT_SLOT_MARKERS"); v != "" {
		cfg.Frontend.StrictSlotMarkers = v == "true" || v == "1"
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
	if v := os.Getenv("SHOPANDA_PAYMENT_STRIPE_WEBHOOK_SECRET"); v != "" {
		cfg.Payment.Stripe.WebhookSecret = v
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
	if v := os.Getenv("SHOPANDA_DEV_EMBED_SCHEDULER"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Dev.EmbedScheduler = b
		}
	}
	if v := os.Getenv("SHOPANDA_METRICS_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Metrics.Enabled = b
		}
	}
	if v := os.Getenv("SHOPANDA_METRICS_LISTEN"); v != "" {
		cfg.Metrics.Listen = v
	}
	if v := os.Getenv("SHOPANDA_TRACING_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Tracing.Enabled = b
		}
	}
	if v := os.Getenv("SHOPANDA_TRACING_ENDPOINT"); v != "" {
		cfg.Tracing.Endpoint = v
	}
	if v := os.Getenv("SHOPANDA_TRACING_INSECURE"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Tracing.Insecure = b
		}
	}
	if v := os.Getenv("SHOPANDA_TRACING_SAMPLE_RATIO"); v != "" {
		// f >= 0, not f > 0: 0 is a deliberate "sample nothing" value here
		// too, same as the YAML path (see DefaultTracingSampleRatio) —
		// silently dropping it would apply this override inconsistently
		// depending on whether it came from YAML or the environment.
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			cfg.Tracing.SampleRatio = f
		}
	}
}

func redactSecret(value string) string {
	if value == "" {
		return ""
	}
	return "***"
}

// flatten converts the Config struct into a dot-notation key-value map.
func flatten(cfg *Config) map[string]string {
	m := make(map[string]string)
	m["server.host"] = cfg.Server.Host
	m["server.port"] = strconv.Itoa(cfg.Server.Port)
	m["server.public_base_url"] = cfg.Server.PublicBaseURL
	m["http.max_body_bytes"] = strconv.FormatInt(cfg.HTTP.MaxBodyBytes, 10)
	m["http.media_max_body_bytes"] = strconv.FormatInt(cfg.HTTP.MediaMaxBodyBytes, 10)
	m["database.host"] = cfg.Database.Host
	m["database.port"] = strconv.Itoa(cfg.Database.Port)
	m["database.user"] = cfg.Database.User
	m["database.password"] = cfg.Database.Password
	m["database.name"] = cfg.Database.Name
	m["database.sslmode"] = cfg.Database.SSLMode
	m["log.level"] = cfg.Log.Level
	m["log.format"] = cfg.Log.Format
	m["auth.jwt_ttl"] = cfg.Auth.JWTTTL
	m["auth.lockout.enabled"] = strconv.FormatBool(cfg.Auth.Lockout.Enabled)
	m["auth.lockout.store"] = cfg.Auth.Lockout.Store
	m["auth.lockout.max_failures"] = strconv.Itoa(cfg.Auth.Lockout.MaxFailures)
	m["auth.lockout.window"] = cfg.Auth.Lockout.Window
	m["mail.driver"] = cfg.Mail.Driver
	m["mail.smtp.host"] = cfg.Mail.SMTP.Host
	m["mail.smtp.port"] = strconv.Itoa(cfg.Mail.SMTP.Port)
	m["mail.smtp.user"] = cfg.Mail.SMTP.User
	m["mail.smtp.from"] = cfg.Mail.SMTP.From
	m["media.storage"] = cfg.Media.Storage
	m["media.local.base_path"] = cfg.Media.Local.BasePath
	m["media.local.base_url"] = cfg.Media.Local.BaseURL
	for name, tc := range cfg.Media.Thumbnails {
		prefix := "media.thumbnails." + name
		m[prefix+".width"] = strconv.Itoa(tc.Width)
		m[prefix+".height"] = strconv.Itoa(tc.Height)
		m[prefix+".fit"] = tc.Fit
	}
	m["media.s3.endpoint"] = cfg.Media.S3.Endpoint
	m["media.s3.bucket"] = cfg.Media.S3.Bucket
	m["media.s3.region"] = cfg.Media.S3.Region
	m["media.s3.base_url"] = cfg.Media.S3.BaseURL
	m["media.s3.public_acl"] = strconv.FormatBool(cfg.Media.S3.PublicACL)
	m["media.webp.enabled"] = strconv.FormatBool(cfg.Media.WebP.Enabled)
	m["media.webp.quality"] = strconv.Itoa(cfg.Media.WebP.Quality)
	m["search.engine"] = cfg.Search.Engine
	m["search.meilisearch.host"] = cfg.Search.Meilisearch.Host
	m["search.meilisearch.index"] = cfg.Search.Meilisearch.Index
	m["cache.driver"] = cfg.Cache.Driver
	m["cache.redis.url"] = cfg.Cache.Redis.URL
	m["cache.redis.key_prefix"] = cfg.Cache.Redis.KeyPrefix
	m["queue.driver"] = cfg.Queue.Driver
	m["queue.redis.url"] = cfg.Queue.Redis.URL
	m["queue.redis.key_prefix"] = cfg.Queue.Redis.KeyPrefix
	m["queue.rabbitmq.url"] = cfg.Queue.RabbitMQ.URL
	m["queue.rabbitmq.queue_prefix"] = cfg.Queue.RabbitMQ.QueuePrefix
	m["queue.kafka.brokers"] = strings.Join(cfg.Queue.Kafka.Brokers, ",")
	m["queue.kafka.topic_prefix"] = cfg.Queue.Kafka.TopicPrefix
	m["queue.sqs.queue_url"] = cfg.Queue.SQS.QueueURL
	m["queue.sqs.failed_queue_url"] = cfg.Queue.SQS.FailedQueueURL
	m["queue.sqs.region"] = cfg.Queue.SQS.Region
	m["plugins.graphql.enabled"] = strconv.FormatBool(cfg.Plugins.GraphQL.Enabled)
	m["plugins.example.enabled"] = strconv.FormatBool(cfg.Plugins.Example.Enabled)
	m["plugins.example.fee_minor_units"] = strconv.FormatInt(cfg.Plugins.Example.FeeMinorUnits, 10)
	m["plugins.slotsdemo.enabled"] = strconv.FormatBool(cfg.Plugins.SlotsDemo.Enabled)
	m["plugins.cartdemo.enabled"] = strconv.FormatBool(cfg.Plugins.CartDemo.Enabled)
	m["plugins.cartdemo.min_quantity"] = strconv.Itoa(cfg.Plugins.CartDemo.MinQuantity)
	m["plugins.cartdemo.handling_fee_minor_units"] = strconv.FormatInt(cfg.Plugins.CartDemo.HandlingFeeMinorUnits, 10)
	m["plugins.taxdemo.enabled"] = strconv.FormatBool(cfg.Plugins.TaxDemo.Enabled)
	m["plugins.maildemo.enabled"] = strconv.FormatBool(cfg.Plugins.MailDemo.Enabled)
	m["plugins.maildemo.subject_prefix"] = cfg.Plugins.MailDemo.SubjectPrefix
	m["plugins.promodemo.enabled"] = strconv.FormatBool(cfg.Plugins.PromoDemo.Enabled)
	m["plugins.taxdemo.flat_rate_bps"] = strconv.Itoa(cfg.Plugins.TaxDemo.FlatRateBPS)
	m["plugins.importdemo.enabled"] = strconv.FormatBool(cfg.Plugins.ImportDemo.Enabled)
	m["plugins.exportdemo.enabled"] = strconv.FormatBool(cfg.Plugins.ExportDemo.Enabled)
	m["plugins.checkoutdemo.enabled"] = strconv.FormatBool(cfg.Plugins.CheckoutDemo.Enabled)
	m["plugins.integrationdemo.enabled"] = strconv.FormatBool(cfg.Plugins.IntegrationDemo.Enabled)
	m["plugins.integrationdemo.integration_api_key"] = cfg.Plugins.IntegrationDemo.IntegrationAPIKey
	m["plugins.integrationdemo.integration_hmac_secret"] = cfg.Plugins.IntegrationDemo.IntegrationHMACSecret
	m["plugins.warehousedemo.enabled"] = strconv.FormatBool(cfg.Plugins.WarehouseDemo.Enabled)
	m["plugins.warehousedemo.warehouse_base_url"] = cfg.Plugins.WarehouseDemo.WarehouseBaseURL
	m["plugins.warehousedemo.warehouse_api_key"] = redactSecret(cfg.Plugins.WarehouseDemo.WarehouseAPIKey)
	m["plugins.warehousedemo.sync_cron"] = cfg.Plugins.WarehouseDemo.SyncCron
	m["plugins.pimdemo.enabled"] = strconv.FormatBool(cfg.Plugins.PimDemo.Enabled)
	m["plugins.pimdemo.pim_graphql_endpoint"] = cfg.Plugins.PimDemo.PimGraphQLEndpoint
	m["plugins.pimdemo.pim_api_key"] = redactSecret(cfg.Plugins.PimDemo.PimAPIKey)
	m["plugins.pimdemo.cache_ttl"] = cfg.Plugins.PimDemo.CacheTTL
	m["plugins.b2b.enabled"] = strconv.FormatBool(cfg.Plugins.B2B.Enabled)
	m["frontend.enabled"] = strconv.FormatBool(cfg.Frontend.Enabled)
	m["frontend.mode"] = cfg.Frontend.Mode
	m["frontend.theme_path"] = cfg.Frontend.ThemePath
	m["frontend.strict_slot_markers"] = strconv.FormatBool(cfg.Frontend.StrictSlotMarkers)
	m["dev.embed_scheduler"] = strconv.FormatBool(cfg.Dev.EmbedScheduler)
	m["cdn.base_url"] = cfg.CDN.BaseURL
	m["payment.stripe.enabled"] = strconv.FormatBool(cfg.Payment.Stripe.Enabled)
	for k, v := range cfg.Webhooks.Secrets {
		m["webhooks.secrets."+k] = v
	}
	m["rate_limit.enabled"] = strconv.FormatBool(cfg.RateLimit.Enabled)
	m["rate_limit.default.rate"] = strconv.FormatFloat(cfg.RateLimit.Default.Rate, 'f', -1, 64)
	m["rate_limit.default.burst"] = strconv.Itoa(cfg.RateLimit.Default.Burst)
	m["metrics.enabled"] = strconv.FormatBool(cfg.Metrics.Enabled)
	m["metrics.listen"] = cfg.Metrics.Listen
	m["tracing.enabled"] = strconv.FormatBool(cfg.Tracing.Enabled)
	m["tracing.endpoint"] = cfg.Tracing.Endpoint
	m["tracing.insecure"] = strconv.FormatBool(cfg.Tracing.Insecure)
	m["tracing.sample_ratio"] = strconv.FormatFloat(cfg.Tracing.SampleRatio, 'f', -1, 64)
	// Redacted, unlike webhooks.secrets.* above: those are pre-existing and
	// out of scope here, but tracing.headers is new in this change and
	// commonly carries a collector API key (Grafana Cloud, Honeycomb) — no
	// reason to expose it raw through Get()/GetOrDefault() from day one.
	for k, v := range cfg.Tracing.Headers {
		m["tracing.headers."+k] = redactSecret(v)
	}
	return m
}

// DatabaseDSN is a convenience for building a DSN from env var or config.
// If DATABASE_URL env var is set, it takes precedence.
func DatabaseDSN(cfg *Config) string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return normalizeDatabaseURL(v)
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
		fmt.Sprintf("media.storage=%s media.local.base_path=%s media.local.base_url=%s media.s3.bucket=%s media.s3.region=%s", c.Media.Storage, c.Media.Local.BasePath, c.Media.Local.BaseURL, c.Media.S3.Bucket, c.Media.S3.Region),
		fmt.Sprintf("search.engine=%s", c.Search.Engine),
		fmt.Sprintf("cache.driver=%s", c.Cache.Driver),
		fmt.Sprintf("queue.driver=%s", c.Queue.Driver),
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
