package admin

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// RetentionDaysConfigKey sets how long admin audit rows are kept (0 = retain forever).
const RetentionDaysConfigKey = "audit.retention_days"

// ConfigGetter reads a single config key.
type ConfigGetter interface {
	Get(ctx context.Context, key string) (interface{}, error)
}

// RetentionDays returns configured audit retention in days. Missing or zero disables pruning.
func RetentionDays(ctx context.Context, repo ConfigGetter) (int, error) {
	if repo == nil {
		return 0, nil
	}
	v, err := repo.Get(ctx, RetentionDaysConfigKey)
	if err != nil {
		return 0, fmt.Errorf("audit retention config: %w", err)
	}
	if v == nil {
		return 0, nil
	}
	days, err := parseRetentionDays(v)
	if err != nil {
		return 0, fmt.Errorf("audit retention config: %w", err)
	}
	return days, nil
}

func parseRetentionDays(v interface{}) (int, error) {
	switch t := v.(type) {
	case int:
		if t < 0 {
			return 0, fmt.Errorf("invalid value %d", t)
		}
		return t, nil
	case int64:
		if t < 0 {
			return 0, fmt.Errorf("invalid value %d", t)
		}
		return int(t), nil
	case float64:
		if t < 0 || float64(int(t)) != t {
			return 0, fmt.Errorf("invalid value %v", v)
		}
		return int(t), nil
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return 0, nil
		}
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("invalid value %q", t)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("invalid value %v", v)
	}
}
