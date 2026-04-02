package identity

import "errors"

// Role represents a user role.
type Role string

const (
	RoleGuest    Role = "guest"
	RoleCustomer Role = "customer"
	RoleAdmin    Role = "admin"
)

// IsValid returns true if r is a recognised role.
func (r Role) IsValid() bool {
	switch r {
	case RoleGuest, RoleCustomer, RoleAdmin:
		return true
	}
	return false
}

// Identity represents an authenticated (or anonymous) user.
type Identity struct {
	UserID string
	Role   Role
}

// NewIdentity creates an Identity with the given user ID and role.
func NewIdentity(userID string, role Role) (Identity, error) {
	if userID == "" {
		return Identity{}, errors.New("identity: user id must not be empty")
	}
	if !role.IsValid() {
		return Identity{}, errors.New("identity: invalid role")
	}
	return Identity{UserID: userID, Role: role}, nil
}

// Guest returns a guest identity (no user ID).
func Guest() Identity {
	return Identity{Role: RoleGuest}
}

// IsGuest returns true if the identity is a guest.
func (i Identity) IsGuest() bool {
	return i.Role == RoleGuest
}

// IsAuthenticated returns true if the identity has a known authenticated
// role (customer or admin) and a non-empty UserID.
func (i Identity) IsAuthenticated() bool {
	if i.UserID == "" {
		return false
	}
	return i.Role == RoleCustomer || i.Role == RoleAdmin
}

// HasRole returns true if the identity has the given role.
func (i Identity) HasRole(role Role) bool {
	return i.Role == role
}
