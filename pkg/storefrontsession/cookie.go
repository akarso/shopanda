// Package storefrontsession shares the guest cart cookie contract between the
// storefront HTTP layer and pluginsdk.CartPort.EnsureStorefrontCart so a
// RegisterPublicRoute handler can reach the same session cart without
// inventing a parallel cookie or putting cart_id in JSON responses.
package storefrontsession

import (
	"net/http"
	"strings"
	"time"
)

// CartCookieName is the HttpOnly cookie that holds the guest storefront cart id.
const CartCookieName = "shopanda_storefront_cart"

// CartCookieMaxAge is 30 days in seconds.
const CartCookieMaxAge = 60 * 60 * 24 * 30

// CartIDFromRequest returns the guest cart id from the storefront cookie.
// Whitespace-only values are treated as absent.
func CartIDFromRequest(r *http.Request) (string, bool) {
	if r == nil {
		return "", false
	}
	c, err := r.Cookie(CartCookieName)
	if err != nil || c == nil {
		return "", false
	}
	id := strings.TrimSpace(c.Value)
	if id == "" {
		return "", false
	}
	return id, true
}

// SetCartCookie writes the guest cart cookie (HttpOnly, Lax).
// Secure follows direct TLS on r (same as IsRequestSecure with nil trusted
// proxies). Prefer SetCartCookieSecure when the caller knows proxy-aware HTTPS.
func SetCartCookie(w http.ResponseWriter, r *http.Request, cartID string) {
	SetCartCookieSecure(w, cartID, r != nil && r.TLS != nil)
}

// SetCartCookieSecure writes the guest cart cookie with an explicit Secure flag
// (use httpshared.IsRequestSecure with trusted proxies from the HTTP layer).
func SetCartCookieSecure(w http.ResponseWriter, cartID string, secure bool) {
	if w == nil || cartID == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CartCookieName,
		Value:    cartID,
		Path:     "/",
		MaxAge:   CartCookieMaxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearCartCookie expires the guest cart cookie.
func ClearCartCookie(w http.ResponseWriter, r *http.Request) {
	ClearCartCookieSecure(w, r != nil && r.TLS != nil)
}

// ClearCartCookieSecure expires the guest cart cookie with an explicit Secure flag.
func ClearCartCookieSecure(w http.ResponseWriter, secure bool) {
	if w == nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CartCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}
