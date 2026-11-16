package storefrontsession

import (
	"net/http"
	"strings"
)

// SessionCookieName is the HttpOnly cookie that holds the storefront customer
// session JWT (same cookie AuthMiddleware reads when Authorization is absent
// or is a non-customer bearer such as a plugin owner token).
const SessionCookieName = "shopanda_storefront_session"

// SessionTokenFromRequest returns the storefront session JWT from the cookie.
// Whitespace-only values are treated as absent.
func SessionTokenFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	c, err := r.Cookie(SessionCookieName)
	if err != nil || c == nil {
		return ""
	}
	return strings.TrimSpace(c.Value)
}
