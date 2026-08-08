package jwt

import (
	"fmt"
	"strings"
	"unicode"
)

const (
	// MinSecretBytes is the minimum accepted secret length (byte length after
	// trailing-whitespace trim). Installer output from `openssl rand -hex 32`
	// is 64 characters and therefore satisfies this minimum as-is.
	MinSecretBytes = 32

	// EnvJWTSecret is the env var name used in validation errors.
	EnvJWTSecret = "SHOPANDA_AUTH_JWT_SECRET"
)

// ParseSecret returns HMAC key material from a configured secret string.
//
// Only trailing Unicode whitespace is stripped (common .env / paste newlines).
// Leading whitespace is preserved so existing secrets that happen to start with
// a whitespace byte keep the same JWT/MFA key material across upgrades.
//
// The result must be at least MinSecretBytes long. Key material is always
// []byte(normalizedSecret) — installer 64-hex is kept as ASCII text, never decoded.
//
// Empty/short values are rejected. Errors name SHOPANDA_AUTH_JWT_SECRET.
func ParseSecret(raw string) ([]byte, error) {
	s := trimTrailingSpace(raw)
	if s == "" {
		return nil, fmt.Errorf("%s: must be set (generate with: openssl rand -hex 32)", EnvJWTSecret)
	}
	if len(s) < MinSecretBytes {
		return nil, fmt.Errorf(
			"%s: must be at least %d bytes (e.g. openssl rand -hex 32); got %d bytes",
			EnvJWTSecret, MinSecretBytes, len(s),
		)
	}
	return []byte(s), nil
}

func trimTrailingSpace(s string) string {
	return strings.TrimRightFunc(s, unicode.IsSpace)
}
