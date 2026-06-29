package mfa

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/customer"
)

const defaultRecoveryCodeCount = 8

// NewRecoveryCodes generates one-time recovery codes and their stored hashes.
func NewRecoveryCodes(count int) (plaintext []string, hashes []string, err error) {
	if count <= 0 {
		count = defaultRecoveryCodeCount
	}
	plaintext = make([]string, 0, count)
	hashes = make([]string, 0, count)
	for i := 0; i < count; i++ {
		code, err := generateRecoveryCode()
		if err != nil {
			return nil, nil, err
		}
		plaintext = append(plaintext, code)
		hashes = append(hashes, customer.HashToken(NormalizeRecoveryCode(code)))
	}
	return plaintext, hashes, nil
}

// NormalizeRecoveryCode strips separators and lowercases hex recovery codes.
func NormalizeRecoveryCode(code string) string {
	out := make([]byte, 0, len(code))
	for i := 0; i < len(code); i++ {
		switch c := code[i]; c {
		case '-', ' ':
			continue
		default:
			out = append(out, c)
		}
	}
	return strings.ToLower(string(out))
}

func generateRecoveryCode() (string, error) {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("mfa: generate recovery code: %w", err)
	}
	return hex.EncodeToString(b), nil
}
