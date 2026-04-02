package devauth

import (
	"context"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/identity"
)

// Parser is a development-only TokenParser.
// It accepts tokens in the format "userID:role" (e.g. "user-1:admin").
type Parser struct{}

// NewParser creates a development token parser.
func NewParser() *Parser {
	return &Parser{}
}

// Parse extracts an Identity from a dev token string.
// The token must be "userID:role" where role is a valid identity.Role.
// The last colon is used as the delimiter so userIDs may contain colons.
func (p *Parser) Parse(_ context.Context, token string) (identity.Identity, error) {
	idx := strings.LastIndex(token, ":")
	if idx < 0 {
		return identity.Identity{}, fmt.Errorf("devauth: invalid token format")
	}

	userID := token[:idx]
	role := identity.Role(token[idx+1:])

	if role == identity.RoleGuest {
		return identity.Identity{}, fmt.Errorf("devauth: guest tokens are not allowed")
	}

	id, err := identity.NewIdentity(userID, role)
	if err != nil {
		return identity.Identity{}, fmt.Errorf("devauth: %w", err)
	}
	return id, nil
}

// Token generates a dev token string for the given user ID and role.
func Token(userID string, role identity.Role) string {
	return userID + ":" + string(role)
}
