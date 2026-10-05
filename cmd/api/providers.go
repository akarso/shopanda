package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	extensionApp "github.com/akarso/shopanda/internal/application/extension"
	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	"github.com/akarso/shopanda/internal/domain/cache"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/domain/tax"
	"github.com/akarso/shopanda/internal/infrastructure/flatrate"
	"github.com/akarso/shopanda/internal/infrastructure/localfs"
	"github.com/akarso/shopanda/internal/infrastructure/manualpay"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	inredis "github.com/akarso/shopanda/internal/infrastructure/redis"
	smtpmail "github.com/akarso/shopanda/internal/infrastructure/smtp"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/metrics"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/internal/platform/ratelimit"
)

func newDiscoveryFacetSyncer(store *adminApp.AttributeStore, engine search.SearchEngine) *adminApp.DiscoveryFacetSyncer {
	var configurer adminApp.AttributeFacetConfigurer
	if c, ok := engine.(adminApp.AttributeFacetConfigurer); ok {
		configurer = c
	}
	return adminApp.NewDiscoveryFacetSyncer(store, configurer)
}

func syncDiscoveryFacetsFromDB(cfg *config.Config, log logger.Logger, conn *sql.DB) error {
	registry := plugin.NewRegistry(log)
	registerPlugins(registry, cfg)
	boot, err := newPluginBootstrap(conn)
	if err != nil {
		return err
	}
	pluginApp := &plugin.App{
		Logger:    log,
		Config:    cfg,
		Bootstrap: boot,
	}
	pluginApp.SetExtensionRegistry(extensionApp.NewRegistry())
	preparePermissionRegistry(pluginApp)
	// Do not abort on unrelated plugin init failures; only the search provider is required.
	if summary := registry.InitAll(pluginApp); summary.Failed > 0 {
		log.Warn("discovery_facet_sync.plugin_init_partial", map[string]interface{}{
			"failed":      summary.Failed,
			"initialized": summary.Initialized,
		})
	}
	freezePermissionRegistry(pluginApp) // discovery facet sync: freeze only (no BindRuntime)
	searchEngine, err := resolveSearchEngine(pluginApp, conn, cfg)
	if err != nil {
		return err
	}
	configRepo := postgres.NewConfigRepo(conn)
	syncer := newDiscoveryFacetSyncer(adminApp.NewAttributeStore(configRepo), searchEngine)
	return syncer.Sync(context.Background())
}

func resolveSearchEngine(app *plugin.App, conn *sql.DB, cfg *config.Config) (search.SearchEngine, error) {
	if se, ok := app.SearchProvider(); ok {
		return se, nil
	}

	switch cfg.Search.Engine {
	case "meilisearch":
		return nil, fmt.Errorf("search: meilisearch engine configured but no search provider registered (core plugin init failed?)")
	case "postgres":
		pgSearch, err := postgres.NewSearchEngine(conn)
		if err != nil {
			return nil, err
		}
		return pgSearch, nil
	default:
		return nil, fmt.Errorf("unsupported search.engine: %q", cfg.Search.Engine)
	}
}

func resolveMediaStorage(app *plugin.App, cfg *config.Config) (media.Storage, error) {
	if st, ok := app.MediaStorage(); ok {
		return st, nil
	}

	switch cfg.Media.Storage {
	case "s3":
		return nil, fmt.Errorf("media: s3 storage configured but no storage plugin registered (core plugin init failed?)")
	case "local":
		return localfs.New(cfg.Media.Local.BasePath, cfg.Media.Local.BaseURL), nil
	default:
		return nil, fmt.Errorf("unsupported media.storage: %s", cfg.Media.Storage)
	}
}

func resolveJobQueue(app *plugin.App, conn *sql.DB, cfg *config.Config) (jobs.Queue, error) {
	if q, ok := app.Queue(); ok {
		return q, nil
	}

	switch cfg.Queue.Driver {
	case "postgres":
		return postgres.NewJobQueue(conn)
	case "redis", "rabbitmq":
		return nil, fmt.Errorf("queue driver %q is not configured (no core plugin registered)", cfg.Queue.Driver)
	default:
		return nil, fmt.Errorf("unsupported queue.driver: %q", cfg.Queue.Driver)
	}
}

func resolveRateLimitFactory(cfg *config.Config, log logger.Logger, rec metrics.Recorder) (ratelimit.Factory, func(), error) {
	if !cfg.RateLimit.Enabled {
		return ratelimit.MemoryFactory(), func() {}, nil
	}
	driver := cfg.RateLimit.Driver
	if driver == "" {
		driver = "memory"
	}
	switch driver {
	case "memory":
		log.Info("ratelimit.driver", map[string]interface{}{"driver": "memory"})
		return ratelimit.MemoryFactory(), func() {}, nil
	case "redis":
		observe := func(f *inredis.LimiterFactory) {
			if rec == nil {
				return
			}
			f.SetErrorObserver(func(name, reason string) {
				rec.RateLimitBackendError(limiterMetricLabel(name), reason)
			})
		}
		url := cfg.RateLimit.RedisURL(cfg.Cache.Redis.URL)
		if url == "" {
			return nil, nil, fmt.Errorf("rate_limit.driver=redis requires rate_limit.redis.url or cache.redis.url (or SHOPANDA_RATE_LIMIT_REDIS_URL / REDIS_URL / SHOPANDA_CACHE_REDIS_URL)")
		}
		client, err := inredis.NewLimiterClient(url, cfg.RateLimit.Redis.PoolSize)
		if err != nil {
			return nil, nil, fmt.Errorf("rate_limit redis: %w", err)
		}
		prefix := cfg.RateLimit.RedisKeyPrefix(cfg.Cache.Redis.KeyPrefix)
		factory := inredis.NewOwnedLimiterFactory(client, prefix, log)
		if err := inredis.PingLimiter(client, 2*time.Second); err != nil {
			if ratelimit.FailClosed(cfg.RateLimit.OnError) {
				_ = factory.Close()
				return nil, nil, fmt.Errorf("rate_limit redis: %w", err)
			}
			fields := map[string]interface{}{"error": err.Error()}
			var closed []string
			for _, r := range cfg.RateLimit.PerRoute {
				if ratelimit.FailClosed(r.OnError) {
					closed = append(closed, r.PathPrefix)
				}
			}
			if len(closed) > 0 {
				fields["fail_closed_routes"] = closed
			}
			log.Warn("ratelimit.redis.unavailable", fields)
			factory.OpenCircuit()
		}
		log.Info("ratelimit.driver", map[string]interface{}{
			"driver":        "redis",
			"key_prefix":    prefix,
			"dedicated":     true,
			"allow_timeout": "200ms",
			"pool_size":     client.Options().PoolSize,
			"url_source":    limiterURLSource(cfg),
		})
		observe(factory)
		return factory, func() { _ = factory.Close() }, nil
	default:
		return nil, nil, fmt.Errorf("unsupported rate_limit.driver: %q", driver)
	}
}

func limiterMetricLabel(name string) string {
	if name == "" || name == "default" {
		return "default"
	}
	return name
}

func limiterURLSource(cfg *config.Config) string {
	if strings.TrimSpace(cfg.RateLimit.Redis.URL) != "" {
		return "rate_limit.redis.url"
	}
	return "cache.redis.url"
}

func resolveCache(app *plugin.App, conn *sql.DB, cfg *config.Config) (cache.Cache, error) {
	if c, ok := app.Cache(); ok {
		return c, nil
	}

	switch cfg.Cache.Driver {
	case "postgres":
		return postgres.NewCacheStore(conn)
	case "redis":
		return nil, fmt.Errorf("cache driver %q is not configured (no core plugin registered)", cfg.Cache.Driver)
	default:
		return nil, fmt.Errorf("unsupported cache.driver: %s", cfg.Cache.Driver)
	}
}

func resolvePaymentRegistry(app *plugin.App) (*payment.ProviderRegistry, error) {
	if reg := app.PaymentRegistry(); reg != nil && reg.Len() > 0 {
		return reg, nil
	}
	reg := payment.NewProviderRegistry()
	reg.Register(manualpay.NewProvider())
	return reg, nil
}

func resolveTaxCalculator(app *plugin.App, rates tax.RateRepository) (tax.Calculator, error) {
	if calc, ok := app.TaxCalculator(); ok {
		return calc, nil
	}
	if rates == nil {
		return nil, fmt.Errorf("tax calculator: rate repository required for core default")
	}
	return appPricing.NewRateTableTaxCalculator(rates, "standard"), nil
}

func resolveShippingRegistry(app *plugin.App) (*shipping.ProviderRegistry, error) {
	if reg := app.ShippingRegistry(); reg != nil && reg.Len() > 0 {
		return reg, nil
	}
	reg := shipping.NewProviderRegistry()
	reg.Register(flatrate.NewProvider(shared.MustNewMoney(500, "USD")))
	return reg, nil
}

func resolveMailer(app *plugin.App, cfg *config.Config) (mail.Mailer, error) {
	if m, ok := app.MailSender(); ok {
		return m, nil
	}
	switch cfg.Mail.Driver {
	case "smtp", "":
		return smtpmail.New(smtpmail.Config{
			Host:     cfg.Mail.SMTP.Host,
			Port:     cfg.Mail.SMTP.Port,
			User:     cfg.Mail.SMTP.User,
			Password: cfg.Mail.SMTP.Password,
			From:     cfg.Mail.SMTP.From,
		}), nil
	default:
		return nil, fmt.Errorf("unsupported mail.driver: %q", cfg.Mail.Driver)
	}
}
