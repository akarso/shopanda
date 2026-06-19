package main

import (
	"database/sql"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/cache"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/infrastructure/localfs"
	"github.com/akarso/shopanda/internal/infrastructure/meili"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/infrastructure/s3store"
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
		me, err := meili.New(meili.Config{
			Host:   cfg.Search.Meilisearch.Host,
			APIKey: cfg.Search.Meilisearch.APIKey,
			Index:  cfg.Search.Meilisearch.Index,
		})
		if err != nil {
			return nil, fmt.Errorf("search: init meilisearch: %w", err)
		}
		return me, nil
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
	case "local":
		return localfs.New(cfg.Media.Local.BasePath, cfg.Media.Local.BaseURL), nil
	case "s3":
		s3s, err := s3store.New(s3store.Config{
			Endpoint:  cfg.Media.S3.Endpoint,
			Bucket:    cfg.Media.S3.Bucket,
			Region:    cfg.Media.S3.Region,
			AccessKey: cfg.Media.S3.AccessKey,
			SecretKey: cfg.Media.S3.SecretKey,
			BaseURL:   cfg.Media.S3.BaseURL,
			PublicACL: cfg.Media.S3.PublicACL,
		})
		if err != nil {
			return nil, fmt.Errorf("media: init s3 storage: %w", err)
		}
		return s3s, nil
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
