package config_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/platform/config"
)

func TestIsPlatformConfigKey(t *testing.T) {
	for _, key := range []string{"queue.driver", "search.engine", "cache.driver", "media.storage"} {
		if !config.IsPlatformConfigKey(key) {
			t.Fatalf("IsPlatformConfigKey(%q) = false, want true", key)
		}
	}
	for _, key := range []string{
		"database.password",
		"queue.redis.url",
		"plugins.example.enabled",
		"media.local.sign_secret",
		"",
	} {
		if config.IsPlatformConfigKey(key) {
			t.Fatalf("IsPlatformConfigKey(%q) = true, want false", key)
		}
	}
}
