package legal

import (
	"context"
	"fmt"
	"strings"
)

// OmnibusEnabledConfigKey toggles EU Omnibus price indication on storefront PDP/PLP.
const OmnibusEnabledConfigKey = "legal.omnibus_enabled"

// ConfigGetter reads a single config key (Postgres config repo or test double).
type ConfigGetter interface {
	Get(ctx context.Context, key string) (interface{}, error)
}

// ScopedConfigKey returns the store-scoped config key used by admin settings.
func ScopedConfigKey(storeID, key string) string {
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return key
	}
	return "store::" + storeID + "::" + key
}

// OmnibusEnabled reports whether Omnibus price indication should run for a store.
// Missing config defaults to true so EU disclosure stays on unless explicitly disabled.
func OmnibusEnabled(ctx context.Context, repo ConfigGetter, storeID string) (bool, error) {
	if repo == nil {
		return true, nil
	}
	if storeID != "" {
		v, err := repo.Get(ctx, ScopedConfigKey(storeID, OmnibusEnabledConfigKey))
		if err != nil {
			return false, fmt.Errorf("omnibus config: store scope: %w", err)
		}
		if v != nil {
			return truthy(v), nil
		}
	}
	v, err := repo.Get(ctx, OmnibusEnabledConfigKey)
	if err != nil {
		return false, fmt.Errorf("omnibus config: global: %w", err)
	}
	if v == nil {
		return true, nil
	}
	return truthy(v), nil
}

func truthy(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.TrimSpace(strings.ToLower(t))
		return s == "true" || s == "1" || s == "yes"
	case float64:
		return t != 0
	case int:
		return t != 0
	case int64:
		return t != 0
	default:
		return false
	}
}
