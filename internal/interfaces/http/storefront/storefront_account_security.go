package storefront

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	"github.com/akarso/shopanda/internal/domain/theme"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
)

const storefrontSecurityVerifyCookieName = "shopanda_storefront_security_verify"

const defaultStorefrontAccountSecurityTTL = 10 * time.Minute
const defaultStorefrontSecurityEmailTokenTTL = 30 * time.Minute
const defaultStorefrontSecurityEmailLinkCooldown = time.Minute
const defaultStorefrontSecurityFreshSessionTTL = 5 * time.Minute

const (
	storefrontEmailTokenPurposeSecurity        = "security_verification"
	storefrontEmailTokenPurposeAccountEmail    = "account_email_verification"
	storefrontEmailTokenPurposeEmailChange     = "account_email_change"
	storefrontEmailVerificationDefaultRedirect = "/account/orders"
)

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
	Purpose    string `json:"purpose"`
	CustomerID string `json:"customer_id"`
	RedirectTo string `json:"redirect_to"`
	ExpiresAt  int64  `json:"expires_at"`
}

type storefrontEmailChangeTokenClaims struct {
	Purpose    string `json:"purpose"`
	CustomerID string `json:"customer_id"`
	NewEmail   string `json:"new_email"`
	Nonce      string `json:"nonce"`
	ExpiresAt  int64  `json:"expires_at"`
}

type storefrontCheckoutResumeTokenClaims struct {
	CustomerID     string                    `json:"customer_id"`
	Step           string                    `json:"step"`
	Address        StorefrontCheckoutAddress `json:"address"`
	ShippingMethod string                    `json:"shipping_method,omitempty"`
	PaymentMethod  string                    `json:"payment_method,omitempty"`
	ExpiresAt      int64                     `json:"expires_at"`
}

type storefrontOrderClaimTokenClaims struct {
	ContactEmail string `json:"contact_email"`
	ExpiresAt    int64  `json:"expires_at"`
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

func (v *storefrontAccountSecurityVerifier) checkoutResumeAESKey() []byte {
	h := sha256.New()
	h.Write([]byte("checkout-resume-aes256gcm:"))
	h.Write(v.secret)
	return h.Sum(nil)
}

func (v *storefrontAccountSecurityVerifier) orderClaimAESKey() []byte {
	h := sha256.New()
	h.Write([]byte("order-claim-aes256gcm:"))
	h.Write(v.secret)
	return h.Sum(nil)
}

func (v *storefrontAccountSecurityVerifier) emailToken(purpose, customerID, redirectTo, defaultRedirectTo string, now time.Time) (string, error) {
	claims := storefrontSecurityEmailTokenClaims{
		Purpose:    strings.TrimSpace(purpose),
		CustomerID: strings.TrimSpace(customerID),
		RedirectTo: storefrontSafeRedirectPath(redirectTo, defaultRedirectTo),
		ExpiresAt:  now.UTC().Add(v.emailTokenTTL).Unix(),
	}
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("storefront security email token: marshal claims: %w", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	return payload + "." + v.signEmailToken(payload), nil
}

func (v *storefrontAccountSecurityVerifier) securityEmailToken(customerID, redirectTo string, now time.Time) (string, error) {
	return v.emailToken(storefrontEmailTokenPurposeSecurity, customerID, redirectTo, "/account/security", now)
}

func (v *storefrontAccountSecurityVerifier) emailVerificationToken(customerID, redirectTo string, now time.Time) (string, error) {
	return v.emailToken(storefrontEmailTokenPurposeAccountEmail, customerID, redirectTo, storefrontEmailVerificationDefaultRedirect, now)
}

// emailChangeToken signs a token carrying the pending new address and its nonce.
func (v *storefrontAccountSecurityVerifier) emailChangeToken(customerID, newEmail, nonce string, now time.Time) (string, error) {
	claims := storefrontEmailChangeTokenClaims{
		Purpose:    storefrontEmailTokenPurposeEmailChange,
		CustomerID: strings.TrimSpace(customerID),
		NewEmail:   strings.ToLower(strings.TrimSpace(newEmail)),
		Nonce:      strings.TrimSpace(nonce),
		ExpiresAt:  now.UTC().Add(v.emailTokenTTL).Unix(),
	}
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("storefront email change token: marshal claims: %w", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	return payload + "." + v.signEmailToken(payload), nil
}

// verifyEmailChangeToken validates signature, purpose, and expiry, returning the
// embedded customer id, new email, and nonce.
func (v *storefrontAccountSecurityVerifier) verifyEmailChangeToken(token string) (string, string, string, bool) {
	if v == nil || strings.TrimSpace(token) == "" {
		return "", "", "", false
	}
	parts := strings.SplitN(strings.TrimSpace(token), ".", 2)
	if len(parts) != 2 {
		return "", "", "", false
	}
	expectedSig := v.signEmailToken(parts[0])
	if !hmac.Equal([]byte(parts[1]), []byte(expectedSig)) {
		return "", "", "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", "", "", false
	}
	var claims storefrontEmailChangeTokenClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return "", "", "", false
	}
	if strings.TrimSpace(claims.Purpose) != storefrontEmailTokenPurposeEmailChange {
		return "", "", "", false
	}
	if time.Unix(claims.ExpiresAt, 0).UTC().Before(time.Now().UTC()) {
		return "", "", "", false
	}
	return strings.TrimSpace(claims.CustomerID), strings.ToLower(strings.TrimSpace(claims.NewEmail)), strings.TrimSpace(claims.Nonce), true
}

// newStorefrontEmailChangeNonce returns a random nonce for email-change tokens.
func newStorefrontEmailChangeNonce() (string, error) {
	buf := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", fmt.Errorf("storefront email change nonce: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (v *storefrontAccountSecurityVerifier) checkoutResumeToken(customerID string, state storefrontCheckoutResumeState, now time.Time) (string, error) {
	claims := storefrontCheckoutResumeTokenClaims{
		CustomerID:     strings.TrimSpace(customerID),
		Step:           storefrontCheckoutResumeStep(state.Step),
		Address:        state.Address,
		ShippingMethod: strings.TrimSpace(state.ShippingMethod),
		PaymentMethod:  strings.TrimSpace(state.PaymentMethod),
		ExpiresAt:      now.UTC().Add(v.emailTokenTTL).Unix(),
	}
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("storefront checkout resume token: marshal claims: %w", err)
	}
	block, err := aes.NewCipher(v.checkoutResumeAESKey())
	if err != nil {
		return "", fmt.Errorf("storefront checkout resume token: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("storefront checkout resume token: gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("storefront checkout resume token: nonce: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(gcm.Seal(nonce, nonce, raw, nil)), nil
}

func (v *storefrontAccountSecurityVerifier) verifyEmailToken(token, purpose string) (string, string, bool) {
	if v == nil || strings.TrimSpace(token) == "" || strings.TrimSpace(purpose) == "" {
		return "", "", false
	}
	parts := strings.SplitN(strings.TrimSpace(token), ".", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	expectedSig := v.signEmailToken(parts[0])
	if !hmac.Equal([]byte(parts[1]), []byte(expectedSig)) {
		return "", "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", "", false
	}
	var claims storefrontSecurityEmailTokenClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return "", "", false
	}
	now := time.Now().UTC()
	claimPurpose := strings.TrimSpace(claims.Purpose)
	if claimPurpose != "" && claimPurpose != strings.TrimSpace(purpose) {
		return "", "", false
	}
	expiresAt := time.Unix(claims.ExpiresAt, 0).UTC()
	if expiresAt.Before(now) {
		return "", "", false
	}
	return strings.TrimSpace(claims.CustomerID), strings.TrimSpace(claims.RedirectTo), true
}

func (v *storefrontAccountSecurityVerifier) verifySecurityEmailToken(token, customerID string) (string, bool) {
	parsedCustomerID, redirectTo, ok := v.verifyEmailToken(token, storefrontEmailTokenPurposeSecurity)
	if !ok || strings.TrimSpace(parsedCustomerID) != strings.TrimSpace(customerID) {
		return "", false
	}
	return storefrontSafeRedirectPath(redirectTo, "/account/security"), true
}

func (v *storefrontAccountSecurityVerifier) verifyCheckoutResumeToken(token, customerID string) (storefrontCheckoutResumeState, bool) {
	if v == nil || strings.TrimSpace(token) == "" || strings.TrimSpace(customerID) == "" {
		return storefrontCheckoutResumeState{}, false
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(token))
	if err != nil {
		return storefrontCheckoutResumeState{}, false
	}
	block, err := aes.NewCipher(v.checkoutResumeAESKey())
	if err != nil {
		return storefrontCheckoutResumeState{}, false
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return storefrontCheckoutResumeState{}, false
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return storefrontCheckoutResumeState{}, false
	}
	raw, err := gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
	if err != nil {
		return storefrontCheckoutResumeState{}, false
	}
	var claims storefrontCheckoutResumeTokenClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return storefrontCheckoutResumeState{}, false
	}
	if strings.TrimSpace(claims.CustomerID) != strings.TrimSpace(customerID) {
		return storefrontCheckoutResumeState{}, false
	}
	if time.Unix(claims.ExpiresAt, 0).UTC().Before(time.Now().UTC()) {
		return storefrontCheckoutResumeState{}, false
	}
	return storefrontCheckoutResumeState{
		Step:           storefrontCheckoutResumeStep(claims.Step),
		Address:        claims.Address,
		ShippingMethod: strings.TrimSpace(claims.ShippingMethod),
		PaymentMethod:  strings.TrimSpace(claims.PaymentMethod),
	}, true
}

func (v *storefrontAccountSecurityVerifier) orderClaimToken(contactEmail string, now time.Time) (string, error) {
	if strings.TrimSpace(contactEmail) == "" {
		return "", fmt.Errorf("storefront order claim token: empty contact email")
	}
	claims := storefrontOrderClaimTokenClaims{
		ContactEmail: strings.ToLower(strings.TrimSpace(contactEmail)),
		ExpiresAt:    now.UTC().Add(v.emailTokenTTL).Unix(),
	}
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("storefront order claim token: marshal claims: %w", err)
	}
	block, err := aes.NewCipher(v.orderClaimAESKey())
	if err != nil {
		return "", fmt.Errorf("storefront order claim token: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("storefront order claim token: gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("storefront order claim token: nonce: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(gcm.Seal(nonce, nonce, raw, nil)), nil
}

func (v *storefrontAccountSecurityVerifier) verifyOrderClaimToken(token string) (string, bool) {
	if v == nil || strings.TrimSpace(token) == "" {
		return "", false
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(token))
	if err != nil {
		return "", false
	}
	block, err := aes.NewCipher(v.orderClaimAESKey())
	if err != nil {
		return "", false
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", false
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", false
	}
	raw, err := gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
	if err != nil {
		return "", false
	}
	var claims storefrontOrderClaimTokenClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return "", false
	}
	if time.Unix(claims.ExpiresAt, 0).UTC().Before(time.Now().UTC()) {
		return "", false
	}
	return strings.ToLower(strings.TrimSpace(claims.ContactEmail)), true
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
		Secure:   httpshared.IsRequestSecure(r, nil),
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
		Secure:   httpshared.IsRequestSecure(r, nil),
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *StorefrontHandler) AccountVerifyEmail() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.auth == nil || h.security == nil || !h.engine.HasTemplate("account_verify_email") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		page := StorefrontAccountEmailVerificationPageData{
			Layout:      h.layoutDataBestEffort(r),
			Theme:       h.engine.Theme(),
			ContinueURL: storefrontSafeRedirectPath(r.URL.Query().Get("redirect_to"), storefrontEmailVerificationDefaultRedirect),
		}
		if token := strings.TrimSpace(r.URL.Query().Get("email_token")); token != "" {
			customerID, redirectTo, ok := h.security.verifyEmailToken(token, storefrontEmailTokenPurposeAccountEmail)
			if !ok {
				page.ContinueURL = "/account/login"
				page.ErrorMessage = "This email verification link is invalid or has expired."
				h.renderPageStatus(w, "account_verify_email", page, http.StatusUnauthorized)
				return
			}
			if err := h.auth.MarkEmailVerified(r.Context(), customerID); err != nil {
				if apperror.Is(err, apperror.CodeNotFound) {
					page.ContinueURL = "/account/login"
					page.ErrorMessage = "This email verification link is invalid or has expired."
					h.renderPageStatus(w, "account_verify_email", page, http.StatusUnauthorized)
					return
				}
				h.log.Error("storefront.account.email_verification_failed", err, map[string]interface{}{
					"customer_id": customerID,
					"path":        r.URL.Path,
				})
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			page.ContinueURL = storefrontSafeRedirectPath(redirectTo, storefrontEmailVerificationDefaultRedirect)
			page.SuccessMessage = "Your email address is verified."
			h.renderPage(w, "account_verify_email", page)
			return
		}
		if r.URL.Query().Get("sent") == "1" {
			page.SuccessMessage = "Check your email for a verification link to confirm this address."
			h.renderPage(w, "account_verify_email", page)
			return
		}
		http.NotFound(w, r)
	}
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
		if !h.requireStorefrontVerifiedEmail(w, r, customerID, "/account/security") {
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
			CSRFToken:  httpshared.CSRFToken(r),
			RedirectTo: storefrontSafeRedirectPath(r.URL.Query().Get("redirect_to"), "/account/security"),
			Email:      profile.Email,
		}
		if token := strings.TrimSpace(r.URL.Query().Get("email_token")); token != "" {
			redirectTo, ok := h.security.verifySecurityEmailToken(token, customerID)
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
			token, err := h.security.securityEmailToken(customerID, page.RedirectTo, now)
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
