package storefrontsession

import (
	"context"
	"net/http"
)

type cookieSecureKey struct{}

// WithCookieSecure records whether Set-Cookie for the guest cart should use
// the Secure flag (typically IsRequestSecure with trusted proxies).
func WithCookieSecure(ctx context.Context, secure bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, cookieSecureKey{}, secure)
}

// CookieSecure returns the Secure bit from ctx when set, else from r.Context(),
// else r.TLS != nil. Prefer the caller's ctx so EnsureStorefrontCart(ctx, …)
// honors WithCookieSecure on either context.
func CookieSecure(ctx context.Context, r *http.Request) bool {
	if ctx != nil {
		if v, ok := ctx.Value(cookieSecureKey{}).(bool); ok {
			return v
		}
	}
	if r != nil {
		if v, ok := r.Context().Value(cookieSecureKey{}).(bool); ok {
			return v
		}
	}
	return r != nil && r.TLS != nil
}
