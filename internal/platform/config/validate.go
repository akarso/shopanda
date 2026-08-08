package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/platform/jwt"
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

	if err := normalizeAuthJWT(&cfg.Auth); err != nil {
		return err
	}
	if err := normalizeAuthLockout(&cfg.Auth.Lockout); err != nil {
		return err
	}
	normalizeHTTP(&cfg.HTTP)

	return nil
}

func normalizeHTTP(h *HTTPConfig) {
	if h.MaxBodyBytes <= 0 {
		h.MaxBodyBytes = DefaultHTTPMaxBodyBytes
	}
	if h.MediaMaxBodyBytes <= 0 {
		h.MediaMaxBodyBytes = DefaultHTTPMediaMaxBodyBytes
	}
	// Caps stay independent: a high JSON limit must not raise the media upload cap.
}

func normalizeAuthJWT(a *AuthConfig) error {
	a.JWTSecret = strings.TrimSpace(a.JWTSecret)
	if _, err := jwt.ParseSecret(a.JWTSecret); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	return nil
}

func normalizeAuthLockout(l *AuthLockoutConfig) error {
	store := strings.ToLower(strings.TrimSpace(l.Store))
	if store == "" {
		store = DefaultAuthLockoutStore
	}
	switch store {
	case "cache", "memory":
		l.Store = store
	default:
		return fmt.Errorf("config: unsupported auth.lockout.store: %q (allowed: cache, memory)", l.Store)
	}
	if l.MaxFailures <= 0 {
		l.MaxFailures = DefaultAuthLockoutMaxFailures
	}
	if strings.TrimSpace(l.Window) == "" {
		l.Window = DefaultAuthLockoutWindow
	}
	d, err := time.ParseDuration(l.Window)
	if err != nil {
		return fmt.Errorf("config: invalid auth.lockout.window %q: %w", l.Window, err)
	}
	if d <= 0 {
		return fmt.Errorf("config: invalid auth.lockout.window %q: must be > 0", l.Window)
	}
	return nil
}
