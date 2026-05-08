package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/akarso/shopanda/internal/domain/theme"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
)

const storefrontSecurityVerifyCookieName = "shopanda_storefront_security_verify"

const defaultStorefrontAccountSecurityTTL = 10 * time.Minute
const defaultStorefrontSecurityEmailTokenTTL = 30 * time.Minute
const defaultStorefrontSecurityEmailLinkCooldown = time.Minute
const defaultStorefrontSecurityFreshSessionTTL = 5 * time.Minute

type storefrontAccountSecurityVerifier struct {
	secret            []byte
	storeBaseURL      string
	ttl               time.Duration
	emailTokenTTL     time.Duration
	emailLinkCooldown time.Duration
	freshSessionTTL   time.Duration
	emailLinkMu       sync.Mutex
	lastEmailLinks    map[string]time.Time
}

type StorefrontAccountSecurityVerifyPageData struct {
	Layout         StorefrontLayoutData
	Theme          theme.Theme
	CSRFToken      string
	RedirectTo     string
	Email          string
	SuccessMessage string
	ErrorMessage   string
}

type storefrontSecurityEmailTokenClaims struct {
	CustomerID string `json:"customer_id"`
	RedirectTo string `json:"redirect_to"`
	ExpiresAt  int64  `json:"expires_at"`
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
		secret:            []byte("storefront-security:" + secret),
		storeBaseURL:      "",
		ttl:               ttl,
		emailTokenTTL:     defaultStorefrontSecurityEmailTokenTTL,
		emailLinkCooldown: defaultStorefrontSecurityEmailLinkCooldown,
		freshSessionTTL:   defaultStorefrontSecurityFreshSessionTTL,
		lastEmailLinks:    make(map[string]time.Time),
	}
}

func (v *storefrontAccountSecurityVerifier) canSendEmailLink(customerID string, now time.Time) error {
	if v == nil || v.emailLinkCooldown <= 0 || strings.TrimSpace(customerID) == "" {
		return nil
	}
	v.emailLinkMu.Lock()
	defer v.emailLinkMu.Unlock()
	lastSentAt, ok := v.lastEmailLinks[strings.TrimSpace(customerID)]
	if !ok {
		return nil
	}
	if now.UTC().Sub(lastSentAt.UTC()) < v.emailLinkCooldown {
		return apperror.RateLimited("Please wait before requesting another verification email.")
	}
	return nil
}

func (v *storefrontAccountSecurityVerifier) markEmailLinkSent(customerID string, sentAt time.Time) {
	if v == nil || v.emailLinkCooldown <= 0 || strings.TrimSpace(customerID) == "" {
		return
	}
	v.emailLinkMu.Lock()
	v.lastEmailLinks[strings.TrimSpace(customerID)] = sentAt.UTC()
	v.emailLinkMu.Unlock()
}

func normalizeStorefrontBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("storefront account security email links require a store base URL")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("storefront account security email links invalid base URL: %w", err)
	}
	if !parsed.IsAbs() || strings.TrimSpace(parsed.Host) == "" {
		return "", fmt.Errorf("storefront account security email links require an absolute store base URL")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
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

func (v *storefrontAccountSecurityVerifier) signEmailToken(payload string) string {
	mac := hmac.New(sha256.New, v.secret)
	_, _ = mac.Write([]byte("storefront-security-email|"))
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (v *storefrontAccountSecurityVerifier) emailToken(customerID, redirectTo string, now time.Time) (string, error) {
	claims := storefrontSecurityEmailTokenClaims{
		CustomerID: strings.TrimSpace(customerID),
		RedirectTo: storefrontSafeRedirectPath(redirectTo, "/account/security"),
		ExpiresAt:  now.UTC().Add(v.emailTokenTTL).Unix(),
	}
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("storefront security email token: marshal claims: %w", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	return payload + "." + v.signEmailToken(payload), nil
}

func (v *storefrontAccountSecurityVerifier) verifyEmailToken(token, customerID string) (string, bool) {
	if v == nil || strings.TrimSpace(token) == "" || strings.TrimSpace(customerID) == "" {
		return "", false
	}
	parts := strings.SplitN(strings.TrimSpace(token), ".", 2)
	if len(parts) != 2 {
		return "", false
	}
	expectedSig := v.signEmailToken(parts[0])
	if !hmac.Equal([]byte(parts[1]), []byte(expectedSig)) {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	var claims storefrontSecurityEmailTokenClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return "", false
	}
	now := time.Now().UTC()
	if strings.TrimSpace(claims.CustomerID) != strings.TrimSpace(customerID) {
		return "", false
	}
	expiresAt := time.Unix(claims.ExpiresAt, 0).UTC()
	if expiresAt.Before(now) {
		return "", false
	}
	return storefrontSafeRedirectPath(claims.RedirectTo, "/account/security"), true
}

func storefrontAbsoluteURL(storeBaseURL, path string, query url.Values) (string, error) {
	baseURL, err := normalizeStorefrontBaseURL(storeBaseURL)
	if err != nil {
		return "", err
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("storefront absolute url: parse base URL: %w", err)
	}
	return base.ResolveReference(&url.URL{Path: path, RawQuery: query.Encode()}).String(), nil
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
		if token := strings.TrimSpace(r.URL.Query().Get("email_token")); token != "" {
			redirectTo, ok := h.security.verifyEmailToken(token, customerID)
			if !ok {
				page.ErrorMessage = "This verification link is invalid or has expired."
				h.renderPageStatus(w, "account_security_verify", page, http.StatusUnauthorized)
				return
			}
			h.log.Info("storefront.account.security.verified", map[string]interface{}{
				"verification_method": "email_token",
				"customer_id":         customerID,
				"redirect_to":         redirectTo,
				"verified_at":         time.Now().UTC().Format(time.RFC3339),
			})
			storefrontSetSecurityVerifyCookie(w, r, h.security, customerID)
			http.Redirect(w, r, redirectTo, http.StatusSeeOther)
			return
		}
		if r.URL.Query().Get("email_sent") == "1" {
			page.SuccessMessage = "We sent a secure verification link to your email."
		}
		if h.security.hasFreshSession(r, customerID) {
			http.Redirect(w, r, page.RedirectTo, http.StatusSeeOther)
			return
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
		if r.FormValue("action") == "email_link" {
			now := time.Now().UTC()
			if err := h.security.canSendEmailLink(customerID, now); err != nil {
				page.ErrorMessage = storefrontAccountErrorMessage(err)
				h.renderPageStatus(w, "account_security_verify", page, storefrontAccountErrorStatus(err))
				return
			}
			token, err := h.security.emailToken(customerID, page.RedirectTo, now)
			if err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			verifyURL, err := storefrontAbsoluteURL(h.security.storeBaseURL, "/account/security/verify", url.Values{"email_token": {token}})
			if err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			if err := h.auth.RequestSecurityVerificationLink(r.Context(), customerID, verifyURL); err != nil {
				page.ErrorMessage = storefrontAccountErrorMessage(err)
				h.renderPageStatus(w, "account_security_verify", page, storefrontAccountErrorStatus(err))
				return
			}
			h.security.markEmailLinkSent(customerID, now)
			query := url.Values{}
			query.Set("redirect_to", page.RedirectTo)
			query.Set("email_sent", "1")
			http.Redirect(w, r, "/account/security/verify?"+query.Encode(), http.StatusSeeOther)
			return
		}
		if err := h.auth.VerifyPassword(r.Context(), customerID, r.FormValue("password")); err != nil {
			page.ErrorMessage = storefrontAccountErrorMessage(err)
			h.renderPageStatus(w, "account_security_verify", page, storefrontAccountErrorStatus(err))
			return
		}
		storefrontSetSecurityVerifyCookie(w, r, h.security, customerID)
		http.Redirect(w, r, page.RedirectTo, http.StatusSeeOther)
	}
}
