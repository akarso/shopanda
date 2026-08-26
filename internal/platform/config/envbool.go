package config

import (
	"os"
	"strings"
)

// EnvTruthy reports whether the named environment variable is explicitly truthy:
// "1", "true", or "yes" (case-insensitive). Absent, empty, and other values are false.
func EnvTruthy(name string) bool {
	return parseEnvBool(os.Getenv(name))
}

// parseEnvBool interprets an already-fetched environment variable value
// using the same case-insensitive "1"/"true"/"yes" convention as EnvTruthy.
// For callers in applyEnv that fetch the value themselves to guard on
// "was this even set" (if v := os.Getenv(NAME); v != "" { ... }) before
// deciding what it means — every such boolean flag in this file used to
// have its own inline v == "true" || v == "1" (or, for two flags,
// strings.EqualFold(v, "true") || v == "1") comparison, silently rejecting
// "True", "TRUE", or "yes" depending on which copy-pasted version a given
// flag happened to get. That inconsistency is exactly the kind of thing
// that fails an operator's SHOPANDA_AUTH_MFA_ENABLED=True silently, with
// no error or log — this is the single place that decides truthiness now.
func parseEnvBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
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

// MetricsInsecureBindAllowed reports whether a non-loopback metrics.listen
// bind is permitted independently of SHOPANDA_DEV_MODE. Kept as its own flag
// (rather than folded into dev mode) so that operators who need external
// Prometheus scraping are not forced to also disable the DB password/SSL
// checks that SHOPANDA_DEV_MODE gates — those are unrelated concerns.
func MetricsInsecureBindAllowed() bool {
	return DevModeEnabled() || EnvTruthy("SHOPANDA_METRICS_ALLOW_INSECURE_BIND")
}
