package legal

import (
	"context"
	"fmt"
	"strings"
)

// Config keys for OSS/IOSS tax reporting (store-scoped via ScopedConfigKey).
const (
	OssEnabledConfigKey = "legal.oss_enabled"
)

// OssEnabled reports whether OSS/IOSS export helpers are enabled for a store.
// Missing config defaults to false (opt-in).
func OssEnabled(ctx context.Context, repo ConfigGetter, storeID string) (bool, error) {
	if repo == nil {
		return false, nil
	}
	if storeID != "" {
		v, err := repo.Get(ctx, ScopedConfigKey(storeID, OssEnabledConfigKey))
		if err != nil {
			return false, fmt.Errorf("oss config: store scope: %w", err)
		}
		if v != nil {
			return truthy(v), nil
		}
	}
	v, err := repo.Get(ctx, OssEnabledConfigKey)
	if err != nil {
		return false, fmt.Errorf("oss config: global: %w", err)
	}
	if v == nil {
		return false, nil
	}
	return truthy(v), nil
}

// NormalizeCountryCode uppercases and trims a 2-letter ISO country code.
func NormalizeCountryCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}
