package auth

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/jwt"
)

// ValidatingTokenParser implements auth.TokenParser by verifying the JWT
// signature and checking the token generation against the stored customer.
type ValidatingTokenParser struct {
	issuer    *jwt.Issuer
	customers customer.CustomerRepository
}

// NewValidatingTokenParser creates a token parser that validates JWT
// signature and checks token generation against the customer record.
func NewValidatingTokenParser(issuer *jwt.Issuer, customers customer.CustomerRepository) *ValidatingTokenParser {
	return &ValidatingTokenParser{issuer: issuer, customers: customers}
}

// Parse extracts an Identity from a JWT bearer token, verifying that the
// token generation matches the customer's current generation.
func (p *ValidatingTokenParser) Parse(ctx context.Context, token string) (identity.Identity, error) {
	claims, err := p.issuer.Parse(token)
	if err != nil {
		return identity.Identity{}, fmt.Errorf("validating parser: %w", err)
	}

	c, err := p.customers.FindByID(ctx, claims.Sub)
	if err != nil {
		return identity.Identity{}, fmt.Errorf("validating parser: %w", err)
	}
	if c == nil {
		return identity.Identity{}, apperror.Unauthorized("invalid token")
	}
	if c.TokenGeneration != claims.Gen {
		return identity.Identity{}, apperror.Unauthorized("token revoked")
	}

	role := identity.Role(claims.Role)
	id, err := identity.NewIdentity(claims.Sub, role)
	if err != nil {
		return identity.Identity{}, fmt.Errorf("validating parser: %w", err)
	}
	return id, nil
}
