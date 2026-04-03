package customer

import "context"

// PasswordResetRepository defines persistence operations for password reset tokens.
type PasswordResetRepository interface {
	// Create persists a new password reset token.
	Create(ctx context.Context, t *PasswordResetToken) error

	// FindByTokenHash returns a reset token by its hash.
	// Returns (nil, nil) when not found.
	FindByTokenHash(ctx context.Context, hash string) (*PasswordResetToken, error)

	// MarkUsed sets the used_at timestamp on a reset token.
	MarkUsed(ctx context.Context, id string) error
}
