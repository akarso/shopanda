package main

import (
	"testing"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func TestResolveRateLimiterFactory_MemoryDriverReturnsNilFactory(t *testing.T) {
	for _, driver := range []string{"", "memory", "MEMORY", " Memory "} {
		cfg := &config.Config{RateLimit: config.RateLimitConfig{Enabled: true, Driver: driver}}
		factory, closer, err := resolveRateLimiterFactory(cfg, logger.New("error"))
		if err != nil {
			t.Fatalf("driver %q: resolveRateLimiterFactory: %v", driver, err)
		}
		if factory != nil {
			t.Errorf("driver %q: factory = non-nil, want nil (RateLimitMiddleware's own in-process default)", driver)
		}
		if closer != nil {
			t.Errorf("driver %q: closer = non-nil, want nil (no redis connection opened)", driver)
		}
	}
}

func TestResolveRateLimiterFactory_RedisDriverWithoutURLErrors(t *testing.T) {
	cfg := &config.Config{RateLimit: config.RateLimitConfig{Enabled: true, Driver: "redis"}}
	if _, _, err := resolveRateLimiterFactory(cfg, logger.New("error")); err == nil {
		t.Fatal("expected an error when rate_limit.driver=redis has no rate_limit.redis.url")
	}
}

// TestResolveRateLimiterFactory_RedisDriverWithUnreachableURLDoesNotErrorAtStartup
// pins the code review fix: a syntactically valid but currently
// unreachable Redis URL must NOT fail startup. resolveRateLimiterFactory
// uses redis.NewLazyClient (no eager PING, unlike ConnectURL) so that a
// Redis outage during, say, a rolling restart doesn't take the whole API
// down — Allow already fails open on the resulting connection error at
// request time, so the availability protection this PR is built around
// would otherwise never get a chance to run.
func TestResolveRateLimiterFactory_RedisDriverWithUnreachableURLDoesNotErrorAtStartup(t *testing.T) {
	cfg := &config.Config{RateLimit: config.RateLimitConfig{
		Enabled: true,
		Driver:  "redis",
		Redis:   config.RedisRateLimitConfig{URL: "redis://127.0.0.1:1"},
	}}
	factory, closer, err := resolveRateLimiterFactory(cfg, logger.New("error"))
	if err != nil {
		t.Fatalf("resolveRateLimiterFactory: %v (an unreachable-but-valid URL must not fail startup)", err)
	}
	if factory == nil {
		t.Error("factory = nil, want a redis-backed factory")
	}
	if closer == nil {
		t.Fatal("closer = nil, want the opened redis client so the caller can close it on shutdown")
	}
	if err := closer.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestResolveRateLimiterFactory_RedisDriverWithMalformedURLErrors pins that
// NewLazyClient still fails fast on a genuine config error (bad URL
// syntax) even though it no longer pings — only reachability checks were
// deferred, not URL validation.
func TestResolveRateLimiterFactory_RedisDriverWithMalformedURLErrors(t *testing.T) {
	cfg := &config.Config{RateLimit: config.RateLimitConfig{
		Enabled: true,
		Driver:  "redis",
		Redis:   config.RedisRateLimitConfig{URL: "not a valid url::"},
	}}
	if _, _, err := resolveRateLimiterFactory(cfg, logger.New("error")); err == nil {
		t.Fatal("expected an error for a malformed rate_limit.redis.url")
	}
}

func TestResolveRateLimiterFactory_UnsupportedDriverErrors(t *testing.T) {
	cfg := &config.Config{RateLimit: config.RateLimitConfig{Enabled: true, Driver: "bogus"}}
	if _, _, err := resolveRateLimiterFactory(cfg, logger.New("error")); err == nil {
		t.Fatal("expected an error for an unsupported rate_limit.driver")
	}
}

// TestResolveRateLimiterFactory_DisabledSkipsConnectingEvenWithRedisDriver
// pins the code review fix: rate_limit.enabled=false must skip connecting
// to Redis entirely, even when rate_limit.driver=redis is configured —
// RateLimitMiddleware never calls the factory when disabled (it
// short-circuits to a passthrough first), so connecting here would only
// risk failing startup over a driver an operator isn't even using yet.
// A missing/invalid URL that would otherwise error is not enough to
// produce an error here, proving the connection attempt never happens.
func TestResolveRateLimiterFactory_DisabledSkipsConnectingEvenWithRedisDriver(t *testing.T) {
	cfg := &config.Config{RateLimit: config.RateLimitConfig{
		Enabled: false,
		Driver:  "redis",
		// No URL at all — would error if the redis branch were reached.
	}}
	factory, closer, err := resolveRateLimiterFactory(cfg, logger.New("error"))
	if err != nil {
		t.Fatalf("resolveRateLimiterFactory: %v (disabled must skip the driver switch entirely)", err)
	}
	if factory != nil {
		t.Error("factory = non-nil, want nil when rate_limit.enabled=false")
	}
	if closer != nil {
		t.Error("closer = non-nil, want nil when rate_limit.enabled=false")
	}
}
