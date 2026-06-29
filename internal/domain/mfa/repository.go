package mfa

import (
	"context"
	"time"
)

// State holds persisted MFA enrollment for a customer.
type State struct {
	SecretEnc   string
	ConfirmedAt *time.Time
}

// Enrolled reports whether TOTP MFA is active for the account.
func (s State) Enrolled() bool {
	return s.ConfirmedAt != nil && s.SecretEnc != ""
}

// Repository persists MFA enrollment and recovery codes.
type Repository interface {
	GetState(ctx context.Context, customerID string) (State, error)
	SavePendingSecret(ctx context.Context, customerID, secretEnc string) error
	// FinalizeEnrollment atomically confirms enrollment and stores recovery code hashes.
	FinalizeEnrollment(ctx context.Context, customerID string, confirmedAt time.Time, codeHashes []string) error
	ClearEnrollment(ctx context.Context, customerID string) error
	ReplaceRecoveryCodes(ctx context.Context, customerID string, codeHashes []string) error
	ConsumeRecoveryCode(ctx context.Context, customerID, codeHash string) (bool, error)
}
