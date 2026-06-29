package mfasec

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const pendingLoginPurpose = "admin_mfa_login"

// PendingLoginClaims identifies an authenticated password step awaiting MFA.
type PendingLoginClaims struct {
	CustomerID     string `json:"customer_id"`
	TokenGeneration int64  `json:"token_generation"`
	ExpiresAt      int64  `json:"expires_at"`
}

// SignPendingLogin creates a short-lived pending login token.
func SignPendingLogin(jwtSecret string, claims PendingLoginClaims, ttl time.Duration) (string, time.Time, error) {
	if strings.TrimSpace(claims.CustomerID) == "" {
		return "", time.Time{}, fmt.Errorf("mfasec: pending login: empty customer id")
	}
	expiresAt := time.Now().UTC().Add(ttl)
	claims.ExpiresAt = expiresAt.Unix()
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("mfasec: pending login marshal: %w", err)
	}
	mac := hmac.New(sha256.New, deriveKey(jwtSecret))
	mac.Write([]byte(pendingLoginPurpose))
	mac.Write(payload)
	sig := mac.Sum(nil)
	token := base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig)
	return token, expiresAt, nil
}

// VerifyPendingLogin validates and parses a pending login token.
func VerifyPendingLogin(jwtSecret, token string) (PendingLoginClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return PendingLoginClaims{}, fmt.Errorf("mfasec: pending login: malformed token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return PendingLoginClaims{}, fmt.Errorf("mfasec: pending login: decode payload: %w", err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return PendingLoginClaims{}, fmt.Errorf("mfasec: pending login: decode sig: %w", err)
	}
	mac := hmac.New(sha256.New, deriveKey(jwtSecret))
	mac.Write([]byte(pendingLoginPurpose))
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return PendingLoginClaims{}, fmt.Errorf("mfasec: pending login: invalid signature")
	}
	var claims PendingLoginClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return PendingLoginClaims{}, fmt.Errorf("mfasec: pending login unmarshal: %w", err)
	}
	if time.Now().UTC().Unix() > claims.ExpiresAt {
		return PendingLoginClaims{}, fmt.Errorf("mfasec: pending login: expired")
	}
	return claims, nil
}

// FormatExpiry returns RFC3339 for API responses.
func FormatExpiry(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// ParseExpiry parses RFC3339 expiry strings in tests.
func ParseExpiry(raw string) (time.Time, error) {
	return time.Parse(time.RFC3339, raw)
}
