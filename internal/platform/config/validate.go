package config

import (
	"fmt"
	"net"
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
	normalizeMetrics(&cfg.Metrics)
	if err := validateMetricsListen(cfg); err != nil {
		return err
	}

	if err := validateSecureDefaults(cfg); err != nil {
		return err
	}

	return nil
}

// normalizeMetrics defaults a blank listen address so an operator who sets
// metrics.enabled=true without metrics.listen never accidentally binds to
// all interfaces (Go's http.Server treats an empty Addr as ":http").
func normalizeMetrics(m *MetricsConfig) {
	if m.Enabled && strings.TrimSpace(m.Listen) == "" {
		m.Listen = DefaultMetricsListen
	}
}

// validateMetricsListen rejects metrics binds that would expose unauthenticated
// /metrics on all interfaces or on a non-loopback address outside dev mode.
func validateMetricsListen(cfg *Config) error {
	if !cfg.Metrics.Enabled {
		return nil
	}
	listen := strings.TrimSpace(cfg.Metrics.Listen)
	if listen == "" {
		return fmt.Errorf("config: metrics.enabled=true requires metrics.listen")
	}
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("config: invalid metrics.listen %q: %w", listen, err)
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	if isMetricsWildcardBind(host) {
		if MetricsInsecureBindAllowed() {
			return nil
		}
		return fmt.Errorf("config: metrics.listen=%q binds all interfaces; use a loopback address (default %q), enable SHOPANDA_DEV_MODE for local development, or set SHOPANDA_METRICS_ALLOW_INSECURE_BIND=true if you understand /metrics has no authentication", listen, DefaultMetricsListen)
	}
	if isLoopbackHost(host) {
		return nil
	}
	if MetricsInsecureBindAllowed() {
		return nil
	}
	return fmt.Errorf("config: metrics.listen=%q is not loopback; use %q (default) and scrape via localhost, enable SHOPANDA_DEV_MODE for local development, or set SHOPANDA_METRICS_ALLOW_INSECURE_BIND=true if you understand /metrics has no authentication", listen, DefaultMetricsListen)
}

func isMetricsWildcardBind(host string) bool {
	switch host {
	case "", "0.0.0.0", "::":
		return true
	default:
		return false
	}
}

func isLoopbackHost(host string) bool {
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	if host != "localhost" {
		return false
	}
	// "localhost" is only trusted as loopback if it actually resolves that
	// way in this environment — a tampered /etc/hosts or DNS override could
	// otherwise point it at a non-loopback address while a bare string
	// comparison still waved it through. Falls back to trusting the literal
	// if resolution itself fails, so this stays a pure, always-succeeding
	// config check in environments (e.g. minimal containers) where local
	// hostname resolution isn't set up, rather than a hard resolver
	// dependency.
	addrs, err := net.LookupHost(host)
	if err != nil {
		return true
	}
	if len(addrs) == 0 {
		return true
	}
	for _, addr := range addrs {
		resolvedIP := net.ParseIP(addr)
		if resolvedIP == nil || !resolvedIP.IsLoopback() {
			return false
		}
	}
	return true
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
		keywords, err := databaseURLToLibpqKeywords(raw)
		if err != nil {
			return "", "", "", err
		}
		return parseLibpqKeywordSecurity(keywords)
	}
	return parseLibpqKeywordSecurity(raw)
}

// databaseURLToLibpqKeywords mirrors lib/pq convertURL so validation sees the same
// keys the driver will apply (including query host=/hostaddr=/sslmode= overrides).
func databaseURLToLibpqKeywords(raw string) (string, error) {
	u, err := url.Parse(normalizeDatabaseURL(raw))
	if err != nil {
		return "", fmt.Errorf("config: invalid DATABASE_URL: %w", err)
	}
	if u == nil {
		return "", fmt.Errorf("config: invalid DATABASE_URL")
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", fmt.Errorf("config: invalid DATABASE_URL scheme %q", u.Scheme)
	}

	var parts []string
	add := func(k, v string) {
		if v == "" {
			return
		}
		parts = append(parts, k+"="+quoteLibpqValue(v))
	}
	if u.User != nil {
		add("user", u.User.Username())
		if p, ok := u.User.Password(); ok {
			add("password", p)
		}
	}
	if host, port, err := net.SplitHostPort(u.Host); err != nil {
		add("host", u.Host)
	} else {
		add("host", host)
		add("port", port)
	}
	if u.Path != "" && u.Path != "/" {
		add("dbname", strings.TrimPrefix(u.Path, "/"))
	}
	// Preserve every query value (not only Get's first) so conflicting sslmode/host
	// can be detected the same way keyword parsing sees repeated keys after convertURL…
	// lib/pq convertURL uses Get (first value only). Emit first value per key to match
	// the driver, then still reject when Values contain conflicting duplicates.
	q := u.Query()
	for k, vals := range q {
		key := strings.ToLower(k)
		if len(vals) == 0 {
			continue
		}
		if key == "sslmode" && conflictingStrings(vals) {
			return "", fmt.Errorf("config: DATABASE_URL has conflicting sslmode values %v", vals)
		}
		if (key == "host" || key == "hostaddr") && conflictingStrings(vals) {
			return "", fmt.Errorf("config: DATABASE_URL has conflicting %s values %v", key, vals)
		}
		// Match lib/pq convertURL: first value wins for the emitted keyword string.
		add(k, vals[0])
	}
	return strings.Join(parts, " "), nil
}

// quoteLibpqValue wraps v in single quotes and escapes \ and ' (lib/pq convertURL style).
func quoteLibpqValue(v string) string {
	escaper := strings.NewReplacer(`\`, `\\`, `'`, `\'`)
	return "'" + escaper.Replace(v) + "'"
}

// unquoteLibpqValue strips matching quotes and unescapes \' and \\.
func unquoteLibpqValue(val string) string {
	val = strings.TrimSpace(val)
	if len(val) < 2 {
		return val
	}
	q := val[0]
	if (q != '\'' && q != '"') || val[len(val)-1] != byte(q) {
		return val
	}
	inner := val[1 : len(val)-1]
	var b strings.Builder
	b.Grow(len(inner))
	escape := false
	for _, r := range inner {
		if escape {
			b.WriteRune(r)
			escape = false
			continue
		}
		if r == '\\' {
			escape = true
			continue
		}
		b.WriteRune(r)
	}
	if escape {
		b.WriteByte('\\')
	}
	return b.String()
}

func conflictingStrings(vals []string) bool {
	if len(vals) < 2 {
		return false
	}
	first := strings.ToLower(strings.TrimSpace(vals[0]))
	for _, v := range vals[1:] {
		if strings.ToLower(strings.TrimSpace(v)) != first {
			return true
		}
	}
	return false
}

func parseLibpqKeywordSecurity(raw string) (host, password, sslmode string, err error) {
	// libpq keyword/value DSN: last key wins (same as lib/pq's option map assignment).
	// Quote-aware scan so password='a b' / host values with spaces are preserved.
	seen := false
	var hostName, hostAddr string
	var sslmodes []string
	for _, field := range scanLibpqFields(raw) {
		key, val, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		seen = true
		key = strings.ToLower(strings.TrimSpace(key))
		val = unquoteLibpqValue(val)
		switch key {
		case "host":
			hostName = val
		case "hostaddr":
			// Dial target for locality (libpq); kept separate from host name.
			hostAddr = val
		case "password":
			password = val
		case "sslmode":
			sslmodes = append(sslmodes, val)
		}
	}
	if !seen {
		return "", "", "", fmt.Errorf("config: DATABASE_URL must be a postgres URL or libpq keyword/value DSN")
	}
	if conflictingStrings(sslmodes) {
		return "", "", "", fmt.Errorf("config: DATABASE_URL has conflicting sslmode values %v", sslmodes)
	}
	if len(sslmodes) > 0 {
		sslmode = sslmodes[len(sslmodes)-1]
	}
	// Dial target is hostaddr when set (libpq); host is only a name for auth/TLS verify.
	host = hostName
	if hostAddr != "" {
		host = hostAddr
	}
	// Multi-host lists: any non-local entry makes the destination non-local.
	if strings.Contains(host, ",") {
		for _, part := range strings.Split(host, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if !isLocalDBHost(part) {
				host = part
				break
			}
			host = part
		}
	}
	return host, password, sslmode, nil
}

// scanLibpqFields splits a keyword/value DSN on whitespace while preserving
// single- or double-quoted values (including spaces), matching lib/pq quoting.
func scanLibpqFields(raw string) []string {
	var (
		fields  []string
		cur     strings.Builder
		inQuote rune
		escape  bool
	)
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		fields = append(fields, cur.String())
		cur.Reset()
	}
	for _, r := range raw {
		if escape {
			cur.WriteRune(r)
			escape = false
			continue
		}
		if inQuote != 0 {
			if r == '\\' {
				escape = true
				cur.WriteRune(r)
				continue
			}
			cur.WriteRune(r)
			if r == inQuote {
				inQuote = 0
			}
			continue
		}
		switch r {
		case '\'', '"':
			inQuote = r
			cur.WriteRune(r)
		case ' ', '\t', '\n', '\r':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return fields
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
