package pluginsdk

import (
	"net/http"

	"github.com/akarso/shopanda/internal/platform/auth"
)

// CustomerID returns the signed-in storefront account id from the request
// context, or "" for guests. Includes staff roles: a storefront login JWT
// may carry admin/manager/editor/support. AuthMiddleware prefers a customer
// session cookie over a staff Bearer so an admin token cannot select another
// shopper's cart while that cookie is present.
func CustomerID(r *http.Request) string {
	if r == nil {
		return ""
	}
	return auth.StorefrontActorID(auth.IdentityFrom(r.Context()))
}
