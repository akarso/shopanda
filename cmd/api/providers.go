package main

import (
	"context"
	"database/sql"
	"fmt"

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
	smtpmail "github.com/akarso/shopanda/internal/infrastructure/smtp"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
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
	pluginApp := &plugin.App{
		Logger:    log,
		Config:    cfg,
		Bootstrap: &plugin.Bootstrap{DB: conn},
	}
	pluginApp.SetExtensionRegistry(extensionApp.NewRegistry())
	// Do not abort on unrelated plugin init failures; only the search provider is required.
	if summary := registry.InitAll(pluginApp); summary.Failed > 0 {
		log.Warn("discovery_facet_sync.plugin_init_partial", map[string]interface{}{
			"failed":      summary.Failed,
			"initialized": summary.Initialized,
		})
	}
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
