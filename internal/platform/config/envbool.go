package config

import (
	"os"
	"strings"
)

// EnvTruthy reports whether the named environment variable is explicitly truthy:
// "1", "true", or "yes" (case-insensitive). Absent, empty, and other values are false.
func EnvTruthy(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

// DevModeEnabled reports whether SHOPANDA_DEV_MODE is explicitly truthy.
func DevModeEnabled() bool {
	return EnvTruthy("SHOPANDA_DEV_MODE")
}

// ShouldLogPasswordResetTokens is true only when both SHOPANDA_DEV_MODE and
// SHOPANDA_DEV_LOG_RESET_TOKENS are explicitly truthy.
func ShouldLogPasswordResetTokens() bool {
	return DevModeEnabled() && EnvTruthy("SHOPANDA_DEV_LOG_RESET_TOKENS")
}
