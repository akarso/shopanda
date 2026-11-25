package voucher

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
)

// Crockford base32 without I, L, O, U — easier to read aloud at a till.
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

const (
	codeGroupLen = 4
	codeGroups   = 3
	codeBodyLen  = codeGroupLen * codeGroups // 12 chars before dashes
)

// NormalizeCode uppercases and strips separators/spaces.
func NormalizeCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	var b strings.Builder
	b.Grow(len(code))
	for _, r := range code {
		switch {
		case r == '-' || r == ' ' || r == '_':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// FormatCode inserts dashes: XXXX-XXXX-XXXX.
func FormatCode(normalized string) string {
	n := NormalizeCode(normalized)
	if len(n) != codeBodyLen {
		return n
	}
	return n[0:4] + "-" + n[4:8] + "-" + n[8:12]
}

// ValidateCode accepts a normalized or dashed code.
func ValidateCode(code string) error {
	n := NormalizeCode(code)
	if len(n) != codeBodyLen {
		return fmt.Errorf("voucher: code must be %d characters", codeBodyLen)
	}
	for _, r := range n {
		if !strings.ContainsRune(crockfordAlphabet, r) {
			return errors.New("voucher: code contains invalid characters")
		}
	}
	return nil
}

// GenerateCode returns a random XXXX-XXXX-XXXX code.
func GenerateCode() (string, error) {
	buf := make([]byte, codeBodyLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("voucher: generate code: %w", err)
	}
	out := make([]byte, codeBodyLen)
	for i := range buf {
		out[i] = crockfordAlphabet[int(buf[i])%len(crockfordAlphabet)]
	}
	return FormatCode(string(out)), nil
}
