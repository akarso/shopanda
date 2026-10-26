package storefront

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	appAuth "github.com/akarso/shopanda/internal/application/auth"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// AccountEmailChange handles POST /account/security/email. It is gated by an
// authenticated session, a verified email, and step-up verification — identical
// to the password-change endpoint. A signed confirmation link carrying the new
// address and a fresh nonce is sent to the new address; the change only takes
// effect once that link is confirmed.
func (h *StorefrontHandler) AccountEmailChange() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.auth == nil || h.security == nil || !h.engine.HasTemplate("account_security") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form body", http.StatusBadRequest)
			return
		}
		customerID, ok := h.requireStorefrontAccount(w, r)
		if !ok {
			return
		}
		if !h.requireStorefrontVerifiedEmail(w, r, customerID, "/account/security") {
			return
		}
		if !h.requireStorefrontSecurityVerification(w, r, customerID, "/account/security") {
			return
		}
		profile, err := h.auth.Me(r.Context(), customerID)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		page := storefrontAccountSecurityPage(h, r, profile)

		newEmail := strings.TrimSpace(r.FormValue("new_email"))
		now := time.Now().UTC()
		if err := h.security.canSendEmailLink(customerID, now); err != nil {
			page.EmailErrorMessage = storefrontAccountErrorMessage(err)
			h.renderPageStatus(w, "account_security", page, storefrontAccountErrorStatus(err))
			return
		}
		nonce, err := newStorefrontEmailChangeNonce()
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		token, err := h.security.emailChangeToken(customerID, newEmail, nonce, now)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		confirmURL, err := storefrontAbsoluteURL(h.security.storeBaseURL, "/account/security/email/confirm", url.Values{"email_token": {token}})
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if err := h.auth.RequestEmailChange(r.Context(), appAuth.RequestEmailChangeInput{
			CustomerID: customerID,
			NewEmail:   newEmail,
			Nonce:      nonce,
			VerifyURL:  confirmURL,
		}); err != nil {
			page.EmailErrorMessage = storefrontAccountErrorMessage(err)
			h.renderPageStatus(w, "account_security", page, storefrontAccountErrorStatus(err))
			return
		}
		h.security.markEmailLinkSent(customerID, now)
		http.Redirect(w, r, "/account/security?email_change=sent", http.StatusSeeOther)
	}
}

// AccountEmailChangeConfirm handles GET /account/security/email/confirm. It
// validates the signed link (signature, purpose, expiry) and applies the change
// if the nonce still matches the latest request and the address is still free.
// It does not require an active session, so the link works from any device.
func (h *StorefrontHandler) AccountEmailChangeConfirm() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.auth == nil || h.security == nil || !h.engine.HasTemplate("account_verify_email") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		page := StorefrontAccountEmailVerificationPageData{
			Layout:      h.layoutDataBestEffort(r),
			Theme:       h.engine.Theme(),
			ContinueURL: "/account/login",
		}
		token := strings.TrimSpace(r.URL.Query().Get("email_token"))
		if token == "" {
			http.NotFound(w, r)
			return
		}
		customerID, newEmail, nonce, ok := h.security.verifyEmailChangeToken(token)
		if !ok {
			page.ErrorMessage = "This email change link is invalid or has expired."
			h.renderPageStatus(w, "account_verify_email", page, http.StatusUnauthorized)
			return
		}
		if _, err := h.auth.ConfirmEmailChange(r.Context(), appAuth.ConfirmEmailChangeInput{
			CustomerID: customerID,
			NewEmail:   newEmail,
			Nonce:      nonce,
		}); err != nil {
			switch {
			case apperror.Is(err, apperror.CodeConflict):
				page.ErrorMessage = "That email address is already in use by another account."
				h.renderPageStatus(w, "account_verify_email", page, http.StatusConflict)
			case apperror.Is(err, apperror.CodeUnauthorized):
				page.ErrorMessage = "This email change link is invalid or has expired."
				h.renderPageStatus(w, "account_verify_email", page, http.StatusUnauthorized)
			default:
				h.log.Error("storefront.account.email_change_failed", err, map[string]interface{}{
					"customer_id": customerID,
					"path":        r.URL.Path,
				})
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
			return
		}
		page.ContinueURL = "/account/security"
		page.SuccessMessage = "Your email address has been updated. Sign in with your new address next time."
		h.renderPage(w, "account_verify_email", page)
	}
}
