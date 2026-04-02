package testhelper

import (
	"net/http"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/infrastructure/devauth"
	"github.com/akarso/shopanda/internal/platform/auth"
)

// AuthenticatedRequest sets the Authorization header on r using a dev token
// for the given user ID and role. It also injects the identity into the
// request context so handlers that read from context work without middleware.
func AuthenticatedRequest(r *http.Request, userID string, role identity.Role) *http.Request {
	token := devauth.Token(userID, role)
	r.Header.Set("Authorization", "Bearer "+token)

	id, err := identity.NewIdentity(userID, role)
	if err != nil {
		panic("testhelper: " + err.Error())
	}
	return r.WithContext(auth.WithIdentity(r.Context(), id))
}

// AdminRequest is a convenience wrapper for AuthenticatedRequest with RoleAdmin.
func AdminRequest(r *http.Request, userID string) *http.Request {
	return AuthenticatedRequest(r, userID, identity.RoleAdmin)
}

// CustomerRequest is a convenience wrapper for AuthenticatedRequest with RoleCustomer.
func CustomerRequest(r *http.Request, userID string) *http.Request {
	return AuthenticatedRequest(r, userID, identity.RoleCustomer)
}
