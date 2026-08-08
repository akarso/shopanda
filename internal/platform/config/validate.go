package config

import (
	"fmt"
	"strings"
	"time"
)

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
	case "postgres", "redis", "rabbitmq", "kafka", "sqs":
	default:
		return fmt.Errorf("config: unsupported queue.driver: %q (allowed: postgres, redis, rabbitmq, kafka, sqs)", cfg.Queue.Driver)
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

	if cfg.Media.Storage == "" {
		cfg.Media.Storage = "local"
	}
	switch cfg.Media.Storage {
	case "local", "s3":
	default:
		return fmt.Errorf("config: unsupported media.storage: %q (allowed: local, s3)", cfg.Media.Storage)
	}

	if err := normalizeAuthLockout(&cfg.Auth.Lockout); err != nil {
		return err
	}

	return nil
}

func normalizeAuthLockout(l *AuthLockoutConfig) error {
	if l.Store == "" {
		l.Store = "cache"
	}
	switch strings.ToLower(strings.TrimSpace(l.Store)) {
	case "cache", "memory":
		l.Store = strings.ToLower(strings.TrimSpace(l.Store))
	default:
		return fmt.Errorf("config: unsupported auth.lockout.store: %q (allowed: cache, memory)", l.Store)
	}
	if l.MaxFailures <= 0 {
		l.MaxFailures = 10
	}
	if strings.TrimSpace(l.Window) == "" {
		l.Window = "15m"
	}
	if _, err := time.ParseDuration(l.Window); err != nil {
		return fmt.Errorf("config: invalid auth.lockout.window %q: %w", l.Window, err)
	}
	return nil
}
