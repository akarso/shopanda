package jwt

import (
	"fmt"
	"strings"
)

const (
	// MinSecretBytes is the minimum accepted secret length (byte length of the
	// trimmed configured string). Installer output from `openssl rand -hex 32`
	// is 64 characters and therefore satisfies this minimum as-is.
	MinSecretBytes = 32

	// EnvJWTSecret is the env var name used in validation errors.
	EnvJWTSecret = "SHOPANDA_AUTH_JWT_SECRET"
)

// ParseSecret trims whitespace and returns HMAC key material.
//
// The trimmed secret must be at least MinSecretBytes long. Key material is
// always []byte(trimmedSecret) — the same interpretation used by prior
// releases (installer 64-hex is kept as ASCII text, never decoded).
//
// Empty/short values are rejected. Errors name SHOPANDA_AUTH_JWT_SECRET.
func ParseSecret(raw string) ([]byte, error) {
	s := strings.TrimSpace(raw)
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
