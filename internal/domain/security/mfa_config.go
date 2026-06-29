package security

import (
	"context"
	"fmt"
	"strings"
)

// AdminMFARequiredConfigKey toggles mandatory MFA for admin-panel login.
const AdminMFARequiredConfigKey = "security.admin_mfa_required"

// ConfigGetter reads a single config key.
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

// AdminMFARequired reports whether admin MFA is required at login.
// Missing config defaults to false.
func AdminMFARequired(ctx context.Context, repo ConfigGetter, storeID string) (bool, error) {
	if repo == nil {
		return false, nil
	}
	if storeID != "" {
		v, err := repo.Get(ctx, ScopedConfigKey(storeID, AdminMFARequiredConfigKey))
		if err != nil {
			return false, fmt.Errorf("admin mfa config: store scope: %w", err)
		}
		if v != nil {
			return truthy(v), nil
		}
	}
	v, err := repo.Get(ctx, AdminMFARequiredConfigKey)
	if err != nil {
		return false, fmt.Errorf("admin mfa config: global: %w", err)
	}
	if v == nil {
		return false, nil
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
	default:
		return false
	}
}
