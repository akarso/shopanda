package cart

import (
	"context"
	"fmt"
	"math"
	"time"

	domainCfg "github.com/akarso/shopanda/internal/domain/config"
)

const (
	// ConfigKeyCartRecoveryEnabled toggles abandoned-cart recovery emails.
	ConfigKeyCartRecoveryEnabled = "marketing.cart_recovery.enabled"
	// ConfigKeyCartRecoveryDelayHours is idle hours before a cart qualifies.
	ConfigKeyCartRecoveryDelayHours = "marketing.cart_recovery.delay_hours"
)

const (
	minRecoveryDelayHours = 1
	maxRecoveryDelayHours = 720
)

// RecoverySettings holds runtime cart recovery configuration.
type RecoverySettings struct {
	Enabled    bool
	StaleAfter time.Duration
}

// LoadRecoverySettings reads cart recovery settings from config.
// Missing keys fall back to enabled=true and defaultStaleAfter.
func LoadRecoverySettings(ctx context.Context, repo domainCfg.Repository, defaultStaleAfter time.Duration) (RecoverySettings, error) {
	if defaultStaleAfter <= 0 {
		defaultStaleAfter = DefaultRecoveryStaleAfter
	}
	out := RecoverySettings{
		Enabled:    true,
		StaleAfter: defaultStaleAfter,
	}
	if repo == nil {
		return out, nil
	}

	enabledRaw, err := repo.Get(ctx, ConfigKeyCartRecoveryEnabled)
	if err != nil {
		return RecoverySettings{}, fmt.Errorf("cart recovery settings: enabled: %w", err)
	}
	if enabledRaw != nil {
		enabled, err := coerceConfigBool(enabledRaw)
		if err != nil {
			return RecoverySettings{}, fmt.Errorf("cart recovery settings: enabled: %w", err)
		}
		out.Enabled = enabled
	}

	delayRaw, err := repo.Get(ctx, ConfigKeyCartRecoveryDelayHours)
	if err != nil {
		return RecoverySettings{}, fmt.Errorf("cart recovery settings: delay_hours: %w", err)
	}
	if delayRaw != nil {
		hours, err := coerceConfigInt(delayRaw)
		if err != nil {
			return RecoverySettings{}, fmt.Errorf("cart recovery settings: delay_hours: %w", err)
		}
		if hours < minRecoveryDelayHours || hours > maxRecoveryDelayHours {
			return RecoverySettings{}, fmt.Errorf("cart recovery settings: delay_hours must be between %d and %d", minRecoveryDelayHours, maxRecoveryDelayHours)
		}
		out.StaleAfter = time.Duration(hours) * time.Hour
	}

	return out, nil
}

func coerceConfigBool(raw interface{}) (bool, error) {
	switch v := raw.(type) {
	case bool:
		return v, nil
	case string:
		switch v {
		case "true", "1":
			return true, nil
		case "false", "0":
			return false, nil
		default:
			return false, fmt.Errorf("invalid bool value %q", v)
		}
	default:
		return false, fmt.Errorf("invalid bool type %T", raw)
	}
}

func coerceConfigInt(raw interface{}) (int, error) {
	switch v := raw.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		if math.Trunc(v) != v {
			return 0, fmt.Errorf("expected integer, got %v", v)
		}
		return int(v), nil
	default:
		return 0, fmt.Errorf("invalid int type %T", raw)
	}
}
