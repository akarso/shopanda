package pluginsdk

import (
	"net/http"

	"github.com/akarso/shopanda/internal/platform/auth"
)

// CustomerID returns the signed-in customer id from the request context, or
// "" for guests. AuthMiddleware (and EnsureStorefrontCart) use the same
// identity: a valid customer Bearer, else the storefront session cookie.
func CustomerID(r *http.Request) string {
	if r == nil {
		return ""
	}
	id := auth.IdentityFrom(r.Context())
	if id.IsGuest() {
		return ""
	}
	return id.UserID
}
