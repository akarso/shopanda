package customer

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

// PasswordResetToken represents a one-time-use password reset token.
type PasswordResetToken struct {
	ID         string
	CustomerID string
	TokenHash  string
	ExpiresAt  time.Time
	UsedAt     *time.Time
	CreatedAt  time.Time
}

// NewPasswordResetToken creates a reset token and returns both the
// domain entity (with hashed token) and the plaintext token for delivery.
func NewPasswordResetToken(id, customerID string, ttl time.Duration) (PasswordResetToken, string, error) {
	if id == "" {
		return PasswordResetToken{}, "", errors.New("reset token: id must not be empty")
	}
	if customerID == "" {
		return PasswordResetToken{}, "", errors.New("reset token: customer_id must not be empty")
	}

	plaintext, err := generateToken()
	if err != nil {
		return PasswordResetToken{}, "", err
	}

	now := time.Now().UTC()
	return PasswordResetToken{
		ID:         id,
		CustomerID: customerID,
		TokenHash:  hashToken(plaintext),
		ExpiresAt:  now.Add(ttl),
		CreatedAt:  now,
	}, plaintext, nil
}

// IsExpired returns true if the token has passed its expiry time.
func (t *PasswordResetToken) IsExpired() bool {
	return time.Now().UTC().After(t.ExpiresAt)
}

// IsUsed returns true if the token has already been consumed.
func (t *PasswordResetToken) IsUsed() bool {
	return t.UsedAt != nil
}

// MarkUsed records the token as consumed.
func (t *PasswordResetToken) MarkUsed() error {
	if t.UsedAt != nil {
		return errors.New("reset token: already used")
	}
	now := time.Now().UTC()
	t.UsedAt = &now
	return nil
}

// HashToken returns the SHA-256 hex digest of a plaintext token.
func HashToken(plaintext string) string {
	return hashToken(plaintext)
}

func hashToken(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", errors.New("reset token: failed to generate random bytes")
	}
	return hex.EncodeToString(b), nil
}
