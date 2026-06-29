package mfasec

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const totpPeriod = 30
const totpDigits = 6

// GenerateTOTPSecret returns a base32-encoded TOTP secret.
func GenerateTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("mfasec: generate totp secret: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

// OTPAuthURL builds an otpauth URI for authenticator apps.
func OTPAuthURL(issuer, account, secret string) string {
	q := url.Values{}
	q.Set("secret", normalizeSecret(secret))
	q.Set("issuer", issuer)
	q.Set("period", "30")
	q.Set("digits", "6")
	q.Set("algorithm", "SHA1")
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// ValidateTOTP checks a six-digit code with ±1 step skew.
func ValidateTOTP(passcode, secret string) bool {
	passcode = strings.TrimSpace(passcode)
	if passcode == "" {
		return false
	}
	now := time.Now().UTC()
	for skew := -1; skew <= 1; skew++ {
		counter := now.Unix()/totpPeriod + int64(skew)
		code, err := hotp(normalizeSecret(secret), counter)
		if err == nil && code == passcode {
			return true
		}
	}
	return false
}

// GenerateTOTPCode returns the current TOTP code for tests.
func GenerateTOTPCode(secret string, t time.Time) (string, error) {
	return hotp(normalizeSecret(secret), t.UTC().Unix()/totpPeriod)
}

func hotp(secret string, counter int64) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		return "", err
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := (int(sum[offset]&0x7f) << 24) |
		(int(sum[offset+1]) << 16) |
		(int(sum[offset+2]) << 8) |
		int(sum[offset+3])
	mod := 1
	for i := 0; i < totpDigits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", totpDigits, code%mod), nil
}

func normalizeSecret(secret string) string {
	return strings.ToUpper(strings.TrimSpace(secret))
}
