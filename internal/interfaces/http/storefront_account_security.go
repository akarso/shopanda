package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/theme"
	"github.com/akarso/shopanda/internal/platform/auth"
)

const storefrontSecurityVerifyCookieName = "shopanda_storefront_security_verify"

const defaultStorefrontAccountSecurityTTL = 10 * time.Minute
const defaultStorefrontSecurityFreshSessionTTL = 5 * time.Minute

type storefrontAccountSecurityVerifier struct {
	secret          []byte
	ttl             time.Duration
	freshSessionTTL time.Duration
}

type StorefrontAccountSecurityVerifyPageData struct {
	Layout       StorefrontLayoutData
	Theme        theme.Theme
	CSRFToken    string
	RedirectTo   string
	Email        string
	ErrorMessage string
}

func newStorefrontAccountSecurityVerifier(secret string, ttl time.Duration) *storefrontAccountSecurityVerifier {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil
	}
	if ttl <= 0 {
		ttl = defaultStorefrontAccountSecurityTTL
	}
	return &storefrontAccountSecurityVerifier{
		secret:          []byte("storefront-security:" + secret),
		ttl:             ttl,
		freshSessionTTL: defaultStorefrontSecurityFreshSessionTTL,
	}
}

func (v *storefrontAccountSecurityVerifier) hasFreshSession(r *http.Request, customerID string) bool {
	if v == nil || v.freshSessionTTL <= 0 || r == nil || strings.TrimSpace(customerID) == "" {
		return false
	}
	id := auth.IdentityFrom(r.Context())
	if strings.TrimSpace(id.UserID) != customerID {
		return false
	}
	if id.AuthenticatedAt.IsZero() {
		return false
	}
	authenticatedAt := id.AuthenticatedAt.UTC()
	now := time.Now().UTC()
	if authenticatedAt.After(now) {
		return false
	}
	return now.Sub(authenticatedAt) <= v.freshSessionTTL
}

func (v *storefrontAccountSecurityVerifier) sign(customerID string, verifiedAt int64) string {
	mac := hmac.New(sha256.New, v.secret)
	_, _ = mac.Write([]byte(customerID))
	_, _ = mac.Write([]byte("|"))
	_, _ = mac.Write([]byte(strconv.FormatInt(verifiedAt, 10)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (v *storefrontAccountSecurityVerifier) cookieValue(customerID string, verifiedAt time.Time) string {
	unix := verifiedAt.UTC().Unix()
	return strconv.FormatInt(unix, 10) + "." + v.sign(customerID, unix)
}

func (v *storefrontAccountSecurityVerifier) isVerified(r *http.Request, customerID string) bool {
	if v == nil {
		return true
	}
	if r == nil || strings.TrimSpace(customerID) == "" {
		return false
	}
	cookie, err := r.Cookie(storefrontSecurityVerifyCookieName)
	if err != nil {
		return false
	}
	parts := strings.SplitN(strings.TrimSpace(cookie.Value), ".", 2)
	if len(parts) != 2 {
		return false
	}
	verifiedAt, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return false
	}
	expectedSig := v.sign(customerID, verifiedAt)
	if !hmac.Equal([]byte(parts[1]), []byte(expectedSig)) {
		return false
	}
	verifiedTime := time.Unix(verifiedAt, 0).UTC()
	if verifiedTime.After(time.Now().UTC()) {
		return false
	}
	return time.Since(verifiedTime) <= v.ttl
}

func storefrontSetSecurityVerifyCookie(w http.ResponseWriter, r *http.Request, verifier *storefrontAccountSecurityVerifier, customerID string) {
	if verifier == nil || strings.TrimSpace(customerID) == "" {
		return
	}
	now := time.Now().UTC()
	http.SetCookie(w, &http.Cookie{
		Name:     storefrontSecurityVerifyCookieName,
		Value:    verifier.cookieValue(customerID, now),
		Path:     "/",
		MaxAge:   int(verifier.ttl.Seconds()),
		Expires:  now.Add(verifier.ttl),
		HttpOnly: true,
		Secure:   isRequestSecure(r, nil),
		SameSite: http.SameSiteLaxMode,
	})
}

func storefrontClearSecurityVerifyCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     storefrontSecurityVerifyCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   isRequestSecure(r, nil),
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *StorefrontHandler) requireStorefrontSecurityVerification(w http.ResponseWriter, r *http.Request, customerID, redirectTo string) bool {
	if h.security == nil {
		return true
	}
	if h.security.hasFreshSession(r, customerID) {
		return true
	}
	if h.security.isVerified(r, customerID) {
		return true
	}
	target := storefrontSafeRedirectPath(redirectTo, "/account/security")
	http.Redirect(w, r, "/account/security/verify?redirect_to="+url.QueryEscape(target), http.StatusSeeOther)
	return false
}

func (h *StorefrontHandler) AccountSecurityVerify() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.auth == nil || h.security == nil || !h.engine.HasTemplate("account_security_verify") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		customerID, ok := h.requireStorefrontAccount(w, r)
		if !ok {
			return
		}
		profile, err := h.auth.Me(r.Context(), customerID)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		page := StorefrontAccountSecurityVerifyPageData{
			Layout:     h.layoutDataBestEffort(r),
			Theme:      h.engine.Theme(),
			CSRFToken:  shopandaCSRFToken(r),
			RedirectTo: storefrontSafeRedirectPath(r.URL.Query().Get("redirect_to"), "/account/security"),
			Email:      profile.Email,
		}
		if h.security.isVerified(r, customerID) {
			http.Redirect(w, r, page.RedirectTo, http.StatusSeeOther)
			return
		}
		if r.Method == http.MethodGet {
			h.renderPage(w, "account_security_verify", page)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form body", http.StatusBadRequest)
			return
		}
		page.RedirectTo = storefrontSafeRedirectPath(r.FormValue("redirect_to"), "/account/security")
		if err := h.auth.VerifyPassword(r.Context(), customerID, r.FormValue("password")); err != nil {
			page.ErrorMessage = storefrontAccountErrorMessage(err)
			h.renderPageStatus(w, "account_security_verify", page, storefrontAccountErrorStatus(err))
			return
		}
		storefrontSetSecurityVerifyCookie(w, r, h.security, customerID)
		http.Redirect(w, r, page.RedirectTo, http.StatusSeeOther)
	}
}
