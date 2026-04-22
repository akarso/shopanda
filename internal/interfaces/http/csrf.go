package http

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
)

const storefrontCSRFCookieName = "shopanda_checkout_csrf"

type storefrontCSRFContextKey struct{}

func CSRFMiddleware(trustedProxies ...string) Middleware {
	trustedNets := parseTrustedProxies(trustedProxies)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !storefrontRequiresCSRFPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			token, err := storefrontEnsureCSRFToken(w, r, trustedNets)
			if err != nil {
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}

			if storefrontRequiresCSRFValidation(r.Method) {
				if err := r.ParseForm(); err != nil {
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
				formToken := strings.TrimSpace(r.FormValue("csrf_token"))
				if formToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(formToken)) != 1 {
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
			}

			ctx := context.WithValue(r.Context(), storefrontCSRFContextKey{}, token)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func storefrontCSRFToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	if token, ok := r.Context().Value(storefrontCSRFContextKey{}).(string); ok {
		return token
	}
	cookie, err := r.Cookie(storefrontCSRFCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func storefrontEnsureCSRFToken(w http.ResponseWriter, r *http.Request, trusted []*net.IPNet) (string, error) {
	if cookie, err := r.Cookie(storefrontCSRFCookieName); err == nil && cookie.Value != "" {
		return cookie.Value, nil
	}

	token, err := storefrontGenerateCSRFToken()
	if err != nil {
		return "", err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     storefrontCSRFCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isRequestSecure(r, trusted),
		SameSite: http.SameSiteStrictMode,
	})

	return token, nil
}

func storefrontGenerateCSRFToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func storefrontRequiresCSRFValidation(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

func storefrontRequiresCSRFPath(path string) bool {
	return strings.HasPrefix(path, "/checkout/") || strings.HasPrefix(path, "/account/")
}

func isRequestSecure(r *http.Request, trusted []*net.IPNet) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	if len(trusted) == 0 || !isTrustedProxy(peerIP(r), trusted) {
		return false
	}
	forwardedProto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if i := strings.IndexByte(forwardedProto, ','); i >= 0 {
		forwardedProto = strings.TrimSpace(forwardedProto[:i])
	}
	return strings.EqualFold(forwardedProto, "https")
}
