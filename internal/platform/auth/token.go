package auth

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/identity"
)

// TokenParser extracts an Identity from a bearer token string.
// Returns an error if the token is invalid or expired.
type TokenParser interface {
	Parse(ctx context.Context, token string) (identity.Identity, error)
}
