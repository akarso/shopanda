package main

import (
	"database/sql"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/cache"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/infrastructure/localfs"
	"github.com/akarso/shopanda/internal/infrastructure/manualpay"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

func resolveSearchEngine(app *plugin.App, conn *sql.DB, cfg *config.Config) (search.SearchEngine, error) {
	if v, ok := app.SearchProvider(); ok {
		se, ok := v.(search.SearchEngine)
		if !ok {
			return nil, fmt.Errorf("plugin search provider: invalid type %T", v)
		}
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
	if v, ok := app.MediaStorage(); ok {
		st, ok := v.(media.Storage)
		if !ok {
			return nil, fmt.Errorf("plugin media storage: invalid type %T", v)
		}
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
	if v, ok := app.Queue(); ok {
		q, ok := v.(jobs.Queue)
		if !ok {
			return nil, fmt.Errorf("plugin queue: invalid type %T", v)
		}
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
	if v, ok := app.Cache(); ok {
		c, ok := v.(cache.Cache)
		if !ok {
			return nil, fmt.Errorf("plugin cache: invalid type %T", v)
		}
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
