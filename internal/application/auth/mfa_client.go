package auth

import (
	"context"
	"time"

	"github.com/akarso/shopanda/internal/domain/customer"
)

// MFAClient verifies optional admin MFA during login.
type MFAClient interface {
	RequiredForLogin(ctx context.Context, c *customer.Customer) (bool, error)
	IssuePendingLogin(c *customer.Customer) (token string, expiresAt time.Time, err error)
	CompleteLogin(ctx context.Context, pendingToken, code string) (*customer.Customer, error)
}
