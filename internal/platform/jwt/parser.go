package jwt

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/identity"
)

// TokenParser adapts Issuer to the auth.TokenParser interface.
type TokenParser struct {
	issuer *Issuer
}

// NewTokenParser creates a TokenParser backed by the given Issuer.
func NewTokenParser(issuer *Issuer) *TokenParser {
	return &TokenParser{issuer: issuer}
}

// Parse extracts an Identity from a JWT bearer token.
func (p *TokenParser) Parse(_ context.Context, token string) (identity.Identity, error) {
	claims, err := p.issuer.Parse(token)
	if err != nil {
		return identity.Identity{}, fmt.Errorf("jwt parser: %w", err)
	}

	role := identity.Role(claims.Role)
	id, err := identity.NewIdentity(claims.Sub, role)
	if err != nil {
		return identity.Identity{}, fmt.Errorf("jwt parser: %w", err)
	}
	return id, nil
}
