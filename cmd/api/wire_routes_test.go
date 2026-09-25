package main

import (
	"testing"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func TestResolveRateLimiterFactory_MemoryDriverReturnsNilFactory(t *testing.T) {
	for _, driver := range []string{"", "memory", "MEMORY", " Memory "} {
		cfg := &config.Config{RateLimit: config.RateLimitConfig{Driver: driver}}
		factory, err := resolveRateLimiterFactory(cfg, logger.New("error"))
		if err != nil {
			t.Fatalf("driver %q: resolveRateLimiterFactory: %v", driver, err)
		}
		if factory != nil {
			t.Errorf("driver %q: factory = non-nil, want nil (RateLimitMiddleware's own in-process default)", driver)
		}
	}
}

func TestResolveRateLimiterFactory_RedisDriverWithoutURLErrors(t *testing.T) {
	cfg := &config.Config{RateLimit: config.RateLimitConfig{Driver: "redis"}}
	if _, err := resolveRateLimiterFactory(cfg, logger.New("error")); err == nil {
		t.Fatal("expected an error when rate_limit.driver=redis has no rate_limit.redis.url")
	}
}

func TestResolveRateLimiterFactory_RedisDriverWithUnreachableURLErrors(t *testing.T) {
	cfg := &config.Config{RateLimit: config.RateLimitConfig{
		Driver: "redis",
		Redis:  config.RedisRateLimitConfig{URL: "redis://127.0.0.1:1"},
	}}
	if _, err := resolveRateLimiterFactory(cfg, logger.New("error")); err == nil {
		t.Fatal("expected an error connecting to an unreachable redis URL")
	}
}

func TestResolveRateLimiterFactory_UnsupportedDriverErrors(t *testing.T) {
	cfg := &config.Config{RateLimit: config.RateLimitConfig{Driver: "bogus"}}
	if _, err := resolveRateLimiterFactory(cfg, logger.New("error")); err == nil {
		t.Fatal("expected an error for an unsupported rate_limit.driver")
	}
}
