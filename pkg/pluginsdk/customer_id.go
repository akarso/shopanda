package pluginsdk

import (
	"net/http"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/platform/auth"
)

// CustomerID returns the signed-in storefront customer id from the request
// context, or "" for guests and non-customer roles (admin/manager/editor/
// support). AuthMiddleware may still attach a staff identity for RequireAuth
// / admin checks; only RoleCustomer selects the storefront cart.
func CustomerID(r *http.Request) string {
	if r == nil {
		return ""
	}
	id := auth.IdentityFrom(r.Context())
	if !id.HasRole(identity.RoleCustomer) {
		return ""
	}
	return id.UserID
}
