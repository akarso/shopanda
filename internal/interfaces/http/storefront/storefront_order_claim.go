package storefront

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strings"
	"time"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	orderApp "github.com/akarso/shopanda/internal/application/order"
	"github.com/akarso/shopanda/internal/domain/theme"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// OrderClaimEmailer sends guest-order claim links.
type OrderClaimEmailer interface {
	SendClaimEmail(contactEmail, claimToken string) error
}

// OrderLinker handles guest account registration and order linking.
type OrderLinker interface {
	// RegisterAndLink registers a customer and links a single claimed order.
	RegisterAndLink(r *http.Request, orderID, email, password, firstName, lastName string) (string, string, error)
	// RegisterAndClaimByEmail registers a customer under a verified guest
	// contact email and links all guest orders carrying that email.
	RegisterAndClaimByEmail(r *http.Request, contactEmail, password, firstName, lastName string) (customerID, token string, expiresAt time.Time, err error)
}

// storefrontOrderLinkerAdapter wraps LinkOrderService to implement OrderLinker.
type storefrontOrderLinkerAdapter struct {
	linkService *orderApp.LinkOrderService
}

// NewStorefrontOrderLinkerAdapter creates an adapter that wraps LinkOrderService as OrderLinker.
func NewStorefrontOrderLinkerAdapter(linkService *orderApp.LinkOrderService) OrderLinker {
	return &storefrontOrderLinkerAdapter{linkService: linkService}
}

func (a *storefrontOrderLinkerAdapter) RegisterAndLink(r *http.Request, orderID, email, password, firstName, lastName string) (string, string, error) {
	out, err := a.linkService.RegisterAndLink(r.Context(), orderApp.RegisterAndLinkInput{
		OrderID:   orderID,
		Email:     email,
		Password:  password,
		FirstName: firstName,
		LastName:  lastName,
	})
	if err != nil {
		return "", "", err
	}
	return out.CustomerID, out.Token, nil
}

func (a *storefrontOrderLinkerAdapter) RegisterAndClaimByEmail(r *http.Request, contactEmail, password, firstName, lastName string) (string, string, time.Time, error) {
	out, err := a.linkService.RegisterAndClaimByEmail(r.Context(), orderApp.RegisterAndClaimInput{
		ContactEmail: contactEmail,
		Password:     password,
		FirstName:    firstName,
		LastName:     lastName,
	})
	if err != nil {
		return "", "", time.Time{}, err
	}
	return out.CustomerID, out.Token, out.ExpiresAt, nil
}

type storefrontOrderClaimLinkResponse struct {
	CustomerID string `json:"customer_id"`
	Email      string `json:"email"`
	Token      string `json:"token"`
	Message    string `json:"message"`
}

// StorefrontAccountOrdersClaimPageData renders the guest order claim page.
type StorefrontAccountOrdersClaimPageData struct {
	Layout       StorefrontLayoutData
	Theme        theme.Theme
	CSRFToken    string
	ClaimToken   string
	Email        string
	Orders       []StorefrontAccountOrderRow
	FirstName    string
	LastName     string
	ErrorMessage string
	EmptyMessage string
}

// ClaimOrderSearch handles POST /api/v1/orders/claim-search.
// Guests can request a claim link for their orders by contact email without
// authentication. The response is identical whether or not matching orders
// exist, so the endpoint cannot be used to enumerate customer emails; the
// claim email is only sent when at least one unclaimed guest order matches.
func (h *StorefrontHandler) ClaimOrderSearch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.orderClaim == nil || h.security == nil || h.emailer == nil {
			httpshared.JSONError(w, apperror.NotFound("order claim endpoint not available"))
			return
		}

		if r.Method != http.MethodPost {
			httpshared.JSONError(w, apperror.Validation("POST method required"))
			return
		}

		contactEmail, err := parseClaimSearchInput(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		if contactEmail == "" {
			httpshared.JSONError(w, apperror.Validation("contact_email is required"))
			return
		}

		orders, err := h.orderClaim.SearchGuestOrders(r.Context(), contactEmail)
		if err != nil {
			h.log.Error("storefront.order_claim.search_failed", err, map[string]interface{}{
				"path": r.URL.Path,
			})
			httpshared.JSONError(w, apperror.Internal("failed to search orders"))
			return
		}

		// Failures past this point are logged but still answered with the
		// generic message to keep the response shape constant.
		emailSent := false
		if len(orders) > 0 {
			claimToken, err := h.security.orderClaimToken(contactEmail, time.Now().UTC())
			if err != nil {
				h.log.Error("storefront.order_claim.token_failed", err, map[string]interface{}{
					"path": r.URL.Path,
				})
			} else if err := h.emailer.SendClaimEmail(contactEmail, claimToken); err != nil {
				h.log.Error("storefront.order_claim.email_failed", err, map[string]interface{}{
					"path": r.URL.Path,
				})
			} else {
				emailSent = true
			}
		}
		h.log.Info("storefront.order_claim.search", map[string]interface{}{
			"matches":    len(orders),
			"email_sent": emailSent,
		})

		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"message": "If any orders exist for this email, a claim link has been sent.",
		})
	}
}

// AccountOrdersClaim handles GET and POST /account/orders/claim.
// GET renders the guest's claimable orders for a valid claim token together
// with a registration form. POST registers the account and links all guest
// orders carrying the verified contact email, then signs the customer in.
func (h *StorefrontHandler) AccountOrdersClaim() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.orderClaim == nil || h.security == nil || h.orderLinker == nil || !h.hasTemplate("account_orders_claim") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}

		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "invalid form body", http.StatusBadRequest)
				return
			}
		}
		claimToken := strings.TrimSpace(r.FormValue("claim_token"))
		if claimToken == "" {
			claimToken = strings.TrimSpace(r.URL.Query().Get("claim_token"))
		}

		page := StorefrontAccountOrdersClaimPageData{
			Layout:     h.layoutDataBestEffort(r),
			Theme:      h.themeMeta(),
			CSRFToken:  httpshared.CSRFToken(r),
			ClaimToken: claimToken,
		}

		contactEmail, ok := h.security.verifyOrderClaimToken(claimToken)
		if !ok {
			h.log.Warn("storefront.order_claim.page_invalid_token", map[string]interface{}{
				"path": r.URL.Path,
			})
			page.ClaimToken = ""
			page.ErrorMessage = "This claim link is invalid or has expired. Request a new one from the order search."
			h.renderPageStatus(w, "account_orders_claim", page, http.StatusForbidden)
			return
		}
		page.Email = contactEmail

		orders, err := h.orderClaim.SearchGuestOrders(r.Context(), contactEmail)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		rows := make([]StorefrontAccountOrderRow, 0, len(orders))
		for i := range orders {
			row := storefrontAccountOrderRow(&orders[i])
			row.URL = "" // guests have no order detail page until the order is linked
			rows = append(rows, row)
		}
		page.Orders = rows
		if len(rows) == 0 {
			page.EmptyMessage = "There are no unclaimed orders for this email address."
		}

		if r.Method != http.MethodPost {
			h.log.Info("storefront.order_claim.page", map[string]interface{}{
				"orders": len(rows),
			})
			h.renderPage(w, "account_orders_claim", page)
			return
		}

		page.FirstName = strings.TrimSpace(r.FormValue("first_name"))
		page.LastName = strings.TrimSpace(r.FormValue("last_name"))
		if len(rows) == 0 {
			page.ErrorMessage = "There are no unclaimed orders for this email address."
			h.renderPageStatus(w, "account_orders_claim", page, http.StatusUnprocessableEntity)
			return
		}

		customerID, token, expiresAt, err := h.orderLinker.RegisterAndClaimByEmail(r, contactEmail, r.FormValue("password"), page.FirstName, page.LastName)
		if err != nil {
			h.log.Error("storefront.order_claim.register_failed", err, map[string]interface{}{
				"path":   r.URL.Path,
				"orders": len(rows),
			})
			page.ErrorMessage = storefrontAccountErrorMessage(err)
			h.renderPageStatus(w, "account_orders_claim", page, storefrontAccountErrorStatus(err))
			return
		}

		if err := h.syncStorefrontGuestCart(w, r, customerID); err != nil {
			h.logStorefrontAccountCartSyncFailure("storefront.order_claim.cart_sync_failed", err, r)
		}
		storefrontSetSessionCookie(w, r, token, expiresAt)
		h.log.Info("storefront.order_claim.registered", map[string]interface{}{
			"customer_id": customerID,
			"orders":      len(rows),
		})
		http.Redirect(w, r, "/account/orders", http.StatusSeeOther)
	}
}

// ClaimLink handles POST /api/v1/orders/claim-link.
// Guests can register a new account and link their claimed order in one operation.
func (h *StorefrontHandler) ClaimLink() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.orderLinker == nil || h.security == nil || h.orderClaim == nil {
			httpshared.JSONError(w, apperror.NotFound("order linking endpoint not available"))
			return
		}

		if r.Method != http.MethodPost {
			httpshared.JSONError(w, apperror.Validation("POST method required"))
			return
		}

		in, err := parseClaimRegisterInput(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}

		orderID := in.OrderID
		claimToken := in.ClaimToken
		email := in.Email
		password := in.Password
		firstName := in.FirstName
		lastName := in.LastName

		if orderID == "" {
			httpshared.JSONError(w, apperror.Validation("order_id is required"))
			return
		}
		if claimToken == "" {
			httpshared.JSONError(w, apperror.Validation("claim_token is required"))
			return
		}
		if email == "" {
			httpshared.JSONError(w, apperror.Validation("email is required"))
			return
		}
		if password == "" {
			httpshared.JSONError(w, apperror.Validation("password is required"))
			return
		}

		contactEmail, ok := h.security.verifyOrderClaimToken(claimToken)
		if !ok {
			httpshared.JSONError(w, apperror.Forbidden("invalid or expired claim token"))
			return
		}
		if !strings.EqualFold(contactEmail, email) {
			httpshared.JSONError(w, apperror.Forbidden("order ownership verification failed"))
			return
		}
		if _, err := h.orderClaim.VerifyOrderBelongsToEmail(r.Context(), orderID, contactEmail); err != nil {
			httpshared.JSONError(w, apperror.Forbidden("order ownership verification failed"))
			return
		}

		// Register and link
		customerID, token, err := h.orderLinker.RegisterAndLink(r, orderID, email, password, firstName, lastName)
		if err != nil {
			h.log.Error("storefront.order_claim.link_failed", err, map[string]interface{}{
				"order_id": orderID,
			})
			var appErr *apperror.Error
			if errors.As(err, &appErr) {
				httpshared.JSONError(w, appErr)
				return
			}
			httpshared.JSONError(w, apperror.Internal("failed to register and link order"))
			return
		}
		h.log.Info("storefront.order_claim.linked", map[string]interface{}{
			"order_id":    orderID,
			"customer_id": customerID,
		})

		resp := storefrontOrderClaimLinkResponse{
			CustomerID: customerID,
			Email:      email,
			Token:      token,
			Message:    "Account created successfully. Your order has been linked to your account.",
		}

		httpshared.JSON(w, http.StatusOK, resp)
	}
}

type claimSearchInput struct {
	ContactEmail string `json:"contact_email"`
}

type claimRegisterInput struct {
	OrderID    string `json:"order_id"`
	ClaimToken string `json:"claim_token"`
	Email      string `json:"email"`
	Password   string `json:"password"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
}

func wantsJSONBody(r *http.Request) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return false
	}
	return media == "application/json"
}

func parseClaimSearchInput(r *http.Request) (string, error) {
	if wantsJSONBody(r) {
		var body claimSearchInput
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return "", apperror.Validation("invalid JSON body")
		}
		return strings.ToLower(strings.TrimSpace(body.ContactEmail)), nil
	}
	if err := r.ParseForm(); err != nil {
		return "", apperror.Validation("invalid form payload")
	}
	return strings.ToLower(strings.TrimSpace(r.FormValue("contact_email"))), nil
}

func parseClaimRegisterInput(r *http.Request) (claimRegisterInput, error) {
	var in claimRegisterInput
	if wantsJSONBody(r) {
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			return claimRegisterInput{}, apperror.Validation("invalid JSON body")
		}
	} else {
		if err := r.ParseForm(); err != nil {
			return claimRegisterInput{}, apperror.Validation("invalid form payload")
		}
		in.OrderID = r.FormValue("order_id")
		in.ClaimToken = r.FormValue("claim_token")
		in.Email = r.FormValue("email")
		in.Password = r.FormValue("password")
		in.FirstName = r.FormValue("first_name")
		in.LastName = r.FormValue("last_name")
	}
	in.OrderID = strings.TrimSpace(in.OrderID)
	in.ClaimToken = strings.TrimSpace(in.ClaimToken)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.FirstName = strings.TrimSpace(in.FirstName)
	in.LastName = strings.TrimSpace(in.LastName)
	return in, nil
}
