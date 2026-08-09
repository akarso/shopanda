package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/platform/jwt"
)

// Forbidden local/compose default secrets — rejected unless DevModeEnabled.
var forbiddenDevSecrets = map[string]struct{}{
	"changeme": {},
	"shopanda": {},
}

// Local DB hosts allowed to use sslmode=disable when DevModeEnabled.
// "postgres" is the docker-compose service name for local profiles only.
var localDBHosts = map[string]struct{}{
	"localhost": {},
	"127.0.0.1": {},
	"::1":       {},
	"postgres":  {},
}

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

	if err := validateSecureDefaults(cfg); err != nil {
		return err
	}

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

// validateSecureDefaults rejects weak composed secrets and non-enforcing SSL modes
// (disable/prefer/allow/empty) unless SHOPANDA_DEV_MODE is truthy and the DB host is local.
// Production allowlist: require, verify-ca, verify-full.
func validateSecureDefaults(cfg *Config) error {
	host, password, sslmode, err := effectiveDatabaseSecurity(cfg)
	if err != nil {
		return err
	}
	dev := DevModeEnabled()

	if isForbiddenDevSecret(password) && !dev {
		return fmt.Errorf("config: database.password %q is a forbidden default; set a strong secret or enable SHOPANDA_DEV_MODE=true for local development only", strings.TrimSpace(password))
	}

	sslmode = strings.ToLower(strings.TrimSpace(sslmode))
	if sslmode == "" {
		// Unspecified sslmode is insecure (libpq/pq default is prefer — cleartext fallback).
		sslmode = "disable"
	}
	secureTLS := sslmode == "require" || sslmode == "verify-ca" || sslmode == "verify-full"
	localDev := dev && isLocalDBHost(host)
	if !secureTLS && !localDev {
		return fmt.Errorf("config: database.sslmode=%q is not allowed (use require, verify-ca, or verify-full); disable/prefer/allow only when SHOPANDA_DEV_MODE is truthy and database.host is local (localhost, 127.0.0.1, ::1, or compose service postgres)", sslmode)
	}
	return nil
}

// effectiveDatabaseSecurity returns host/password/sslmode that will actually be used.
// When DATABASE_URL is set, only that DSN is inspected (URL or libpq keyword form) —
// YAML/env database.* values are not merged in, because DatabaseDSN returns the raw URL.
func effectiveDatabaseSecurity(cfg *Config) (host, password, sslmode string, err error) {
	raw := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if raw == "" {
		return cfg.Database.Host, cfg.Database.Password, cfg.Database.SSLMode, nil
	}
	return parseDATABASEURLSecurity(raw)
}

func parseDATABASEURLSecurity(raw string) (host, password, sslmode string, err error) {
	lower := strings.ToLower(strings.TrimSpace(raw))
	if strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://") {
		return parsePostgresURLSecurity(raw)
	}
	return parseLibpqKeywordSecurity(raw)
}

func parsePostgresURLSecurity(raw string) (host, password, sslmode string, err error) {
	u, err := url.Parse(normalizeDatabaseURL(raw))
	if err != nil || u == nil {
		return "", "", "", fmt.Errorf("config: invalid DATABASE_URL: %w", err)
	}
	host = u.Hostname()
	if u.User != nil {
		if p, ok := u.User.Password(); ok {
			password = p
		}
	}
	// Do not fall back to YAML sslmode — DatabaseDSN does not merge it into the URL.
	sslmode = u.Query().Get("sslmode")
	return host, password, sslmode, nil
}

func parseLibpqKeywordSecurity(raw string) (host, password, sslmode string, err error) {
	// libpq keyword/value DSN: "host=... user=... password=... sslmode=..."
	seen := false
	for _, field := range strings.Fields(raw) {
		key, val, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		seen = true
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `'"`)
		switch key {
		case "host", "hostaddr":
			host = val
		case "password":
			password = val
		case "sslmode":
			sslmode = val
		}
	}
	if !seen {
		return "", "", "", fmt.Errorf("config: DATABASE_URL must be a postgres URL or libpq keyword/value DSN")
	}
	return host, password, sslmode, nil
}

func isForbiddenDevSecret(password string) bool {
	_, ok := forbiddenDevSecrets[strings.ToLower(strings.TrimSpace(password))]
	return ok
}

func isLocalDBHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	h = strings.Trim(h, "[]")
	_, ok := localDBHosts[h]
	return ok
}
