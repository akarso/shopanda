package cron

import (
	"fmt"
	"strconv"
	"strings"
)

// NormalizeSpec converts supported cron shorthands to 5-field cron syntax.
// Supports "@every Nm" and "@every Nh" in addition to standard cron specs.
func NormalizeSpec(spec string) (string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", fmt.Errorf("cron spec must not be empty")
	}
	if strings.HasPrefix(spec, "@every ") {
		return parseEverySpec(strings.TrimSpace(spec[7:]))
	}
	if _, err := parse(spec); err != nil {
		return "", err
	}
	return spec, nil
}

func parseEverySpec(body string) (string, error) {
	if body == "" {
		return "", fmt.Errorf("invalid @every spec")
	}
	unit := body[len(body)-1]
	valueStr := strings.TrimSpace(body[:len(body)-1])
	n, err := strconv.Atoi(valueStr)
	if err != nil || n <= 0 {
		return "", fmt.Errorf("invalid @every interval %q", body)
	}
	switch unit {
	case 'm', 'M':
		if n >= 60 {
			return "", fmt.Errorf("@every minutes must be less than 60")
		}
		return fmt.Sprintf("*/%d * * * *", n), nil
	case 'h', 'H':
		if n >= 24 {
			return "", fmt.Errorf("@every hours must be less than 24")
		}
		return fmt.Sprintf("0 */%d * * *", n), nil
	default:
		return "", fmt.Errorf("unsupported @every unit %q (use m or h)", string(unit))
	}
}
