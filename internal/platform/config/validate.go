package config

import "fmt"

func normalizeAndValidate(cfg *Config) error {
	if cfg.Queue.Driver == "" {
		cfg.Queue.Driver = "postgres"
	}
	if cfg.Search.Engine == "" {
		cfg.Search.Engine = "postgres"
	}
	if cfg.Cache.Driver == "" {
		cfg.Cache.Driver = "postgres"
	}

	switch cfg.Queue.Driver {
	case "postgres", "redis", "rabbitmq":
	default:
		return fmt.Errorf("config: unsupported queue.driver: %q (allowed: postgres, redis, rabbitmq)", cfg.Queue.Driver)
	}

	switch cfg.Cache.Driver {
	case "postgres", "redis":
	default:
		return fmt.Errorf("config: unsupported cache.driver: %q (allowed: postgres, redis)", cfg.Cache.Driver)
	}

	switch cfg.Search.Engine {
	case "postgres", "meilisearch":
	default:
		return fmt.Errorf("config: unsupported search.engine: %q (allowed: postgres, meilisearch)", cfg.Search.Engine)
	}

	return nil
}
