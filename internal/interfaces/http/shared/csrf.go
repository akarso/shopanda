package shared

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/platform/apperror"
)

const (
	shopandaCSRFCookieName = "shopanda_csrf"
	// CSRFHeaderName is the header same-origin JSON clients send instead of
	// a form csrf_token field.
	CSRFHeaderName = "X-CSRF-Token"
)

type shopandaCSRFContextKey struct{}

func CSRFMiddleware(trustedProxies ...string) Middleware {
	trustedNets := ParseTrustedProxies(trustedProxies)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !shopandaRequiresCSRFPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			token, err := shopandaEnsureCSRFToken(w, r, trustedNets)
			if err != nil {
				if shopandaStorefrontCSRFPath(r.URL.Path) {
					JSONError(w, apperror.Internal("failed to issue csrf token"))
					return
				}
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}

			if shopandaRequiresCSRFValidation(r.Method) {
				presented := strings.TrimSpace(r.Header.Get(CSRFHeaderName))
				storefrontJSON := shopandaStorefrontCSRFPath(r.URL.Path)
				if presented == "" && !storefrontJSON {
					if err := r.ParseForm(); err != nil {
						http.Error(w, "forbidden", http.StatusForbidden)
						return
					}
					presented = strings.TrimSpace(r.FormValue("csrf_token"))
				}
				if presented == "" || subtle.ConstantTimeCompare([]byte(token), []byte(presented)) != 1 {
					if storefrontJSON {
						JSONError(w, storefrontCSRFForbidden())
						return
					}
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
			}

			ctx := context.WithValue(r.Context(), shopandaCSRFContextKey{}, token)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CSRFToken returns the CSRF token associated with the request, preferring
// the value stashed in context by CSRFMiddleware and falling back to the
// raw cookie (e.g. for handlers that run before the middleware sets context).
func CSRFToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	if token, ok := r.Context().Value(shopandaCSRFContextKey{}).(string); ok {
		return token
	}
	cookie, err := r.Cookie(shopandaCSRFCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// EnsureCSRFToken returns the existing CSRF cookie or mints a new one (Set-Cookie).
// Used by CSRFMiddleware and by /fragments/csrf so cacheable pages can obtain a
// token without visiting /account/* or /checkout/*.
func EnsureCSRFToken(w http.ResponseWriter, r *http.Request, trusted []*net.IPNet) (string, error) {
	return shopandaEnsureCSRFToken(w, r, trusted)
}

func shopandaEnsureCSRFToken(w http.ResponseWriter, r *http.Request, trusted []*net.IPNet) (string, error) {
	if cookie, err := r.Cookie(shopandaCSRFCookieName); err == nil && cookie.Value != "" {
		return cookie.Value, nil
	}

	token, err := generateCSRFToken()
	if err != nil {
		return "", err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     shopandaCSRFCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   IsRequestSecure(r, trusted),
		SameSite: http.SameSiteStrictMode,
	})

	return token, nil
}

var generateCSRFToken = defaultGenerateCSRFToken

func defaultGenerateCSRFToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// SetCSRFTokenGeneratorForTest overrides CSRF minting. Tests must restore via the returned func.
func SetCSRFTokenGeneratorForTest(fn func() (string, error)) (restore func()) {
	prev := generateCSRFToken
	if fn == nil {
		generateCSRFToken = defaultGenerateCSRFToken
	} else {
		generateCSRFToken = fn
	}
	return func() { generateCSRFToken = prev }
}

func shopandaRequiresCSRFValidation(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

func shopandaRequiresCSRFPath(path string) bool {
	return strings.HasPrefix(path, "/checkout/") ||
		strings.HasPrefix(path, "/account/") ||
		shopandaStorefrontCSRFPath(path)
}

func shopandaStorefrontCSRFPath(path string) bool {
	return strings.HasPrefix(path, "/storefront/")
}

func storefrontCSRFForbidden() error {
	return apperror.Forbidden("csrf token missing or invalid")
}

// IsRequestSecure reports whether r arrived over TLS, either directly or via
// a trusted reverse proxy that forwarded X-Forwarded-Proto: https.
func IsRequestSecure(r *http.Request, trusted []*net.IPNet) bool {
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
