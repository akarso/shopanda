package http

import (
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/platform/apperror"
)

type storefrontOrderSearchRequest struct {
	ContactEmail string
}

type storefrontOrderSearchResponse struct {
	ContactEmail string               `json:"contact_email"`
	ClaimToken   string               `json:"claim_token"`
	Orders       []storefrontOrderRef `json:"orders"`
}

type storefrontOrderRef struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	TotalText string `json:"total_text"`
	CreatedAt string `json:"created_at"`
}

type storefrontOrderClaimRequest struct {
	OrderID    string
	ClaimToken string
}

type storefrontOrderClaimResponse struct {
	OrderID string `json:"order_id"`
	Message string `json:"message"`
}

// ClaimOrderSearch handles POST /api/v1/orders/claim-search.
// Guests can search for their orders by contact email without authentication.
func (h *StorefrontHandler) ClaimOrderSearch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.orderClaim == nil || h.security == nil {
			JSONError(w, apperror.NotFound("order claim endpoint not available"))
			return
		}

		if r.Method != http.MethodPost {
			JSONError(w, apperror.Validation("POST method required"))
			return
		}

		// Parse request
		contactEmail := strings.ToLower(strings.TrimSpace(r.FormValue("contact_email")))
		if contactEmail == "" {
			JSONError(w, apperror.Validation("contact_email is required"))
			return
		}

		// Generate claim token for this email (would be sent via email service)
		// TODO: claimToken, err := h.security.orderClaimToken(contactEmail, time.Now().UTC())
		// TODO: h.emailer.SendClaimEmail(contactEmail, claimToken)

		// Return generic success (no orders/token disclosed)
		JSON(w, http.StatusOK, map[string]interface{}{
			"message": "If an account exists for this email, a claim link has been sent.",
		})
	}
}

// ClaimOrder handles POST /api/v1/orders/claim.
// Guests can claim an order and optionally link it to their new/existing authenticated account.
func (h *StorefrontHandler) ClaimOrder() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.orderClaim == nil || h.security == nil {
			JSONError(w, apperror.NotFound("order claim endpoint not available"))
			return
		}

		if r.Method != http.MethodPost {
			JSONError(w, apperror.Validation("POST method required"))
			return
		}

		// Parse request
		orderID := strings.TrimSpace(r.FormValue("order_id"))
		claimToken := strings.TrimSpace(r.FormValue("claim_token"))

		if orderID == "" {
			JSONError(w, apperror.Validation("order_id is required"))
			return
		}
		if claimToken == "" {
			JSONError(w, apperror.Validation("claim_token is required"))
			return
		}

		// Verify claim token
		contactEmail, ok := h.security.verifyOrderClaimToken(claimToken)
		if !ok {
			JSONError(w, apperror.Forbidden("invalid or expired claim token"))
			return
		}

		// Verify order belongs to the contact email
		order, err := h.orderClaim.VerifyOrderBelongsToEmail(r.Context(), orderID, contactEmail)
		if err != nil {
			JSONError(w, apperror.Forbidden("order not found or does not match contact email"))
			return
		}

		// Response - order successfully claimed
		// In a full implementation, here we might:
		// - Create/link customer account if provided
		// - Emit event for order linking workflow
		// - Generate account activation token if new account
		resp := storefrontOrderClaimResponse{
			OrderID: order.ID,
			Message: "Order claimed successfully. You can now register or sign in with this email to view your order.",
		}

		JSON(w, http.StatusOK, resp)
	}
}
