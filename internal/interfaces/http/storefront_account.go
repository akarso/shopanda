package http

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	appAuth "github.com/akarso/shopanda/internal/application/auth"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/theme"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

const (
	storefrontSessionCookieName   = "shopanda_storefront_session"
	storefrontSessionCookieMaxAge = 60 * 60 * 24 * 14
)

type StorefrontAccountLoginPageData struct {
	Layout         StorefrontLayoutData
	Theme          theme.Theme
	CSRFToken      string
	RedirectTo     string
	Email          string
	ErrorMessage   string
	SuccessMessage string
}

type StorefrontAccountRegisterPageData struct {
	Layout         StorefrontLayoutData
	Theme          theme.Theme
	CSRFToken      string
	RedirectTo     string
	FirstName      string
	LastName       string
	Email          string
	ErrorMessage   string
	SuccessMessage string
}

type StorefrontAccountOrderRow struct {
	ID        string
	DateText  string
	TotalText string
	Status    string
	URL       string
}

type StorefrontAccountOrdersPageData struct {
	Layout       StorefrontLayoutData
	Theme        theme.Theme
	Orders       []StorefrontAccountOrderRow
	EmptyMessage string
}

type StorefrontAccountOrderItem struct {
	Name          string
	SKU           string
	Quantity      int
	UnitPriceText string
	LineTotalText string
}

type StorefrontAccountOrderDetailPageData struct {
	Layout    StorefrontLayoutData
	Theme     theme.Theme
	OrderID   string
	DateText  string
	Status    string
	TotalText string
	Items     []StorefrontAccountOrderItem
	BackURL   string
}

type StorefrontAccountProfilePageData struct {
	Layout               StorefrontLayoutData
	Theme                theme.Theme
	CSRFToken            string
	Email                string
	FirstName            string
	LastName             string
	ProfileErrorMessage  string
	PasswordErrorMessage string
	DeleteErrorMessage   string
	SuccessMessage       string
	OrdersURL            string
}

func (h *StorefrontHandler) Login() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.auth == nil || !h.engine.HasTemplate("account_login") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		if storefrontCustomerID(r) != "" {
			http.Redirect(w, r, "/account/orders", http.StatusSeeOther)
			return
		}

		page := StorefrontAccountLoginPageData{
			Layout:     h.layoutDataBestEffort(r),
			Theme:      h.engine.Theme(),
			CSRFToken:  shopandaCSRFToken(r),
			RedirectTo: "/account/orders",
		}
		if r.Method == http.MethodGet {
			page.RedirectTo = storefrontSafeRedirectPath(r.URL.Query().Get("redirect_to"), "/account/orders")
			if r.URL.Query().Get("password_changed") == "1" {
				page.SuccessMessage = "Password changed. Sign in again to continue."
			}
			if r.URL.Query().Get("logged_out") == "1" {
				page.SuccessMessage = "You have been signed out."
			}
			h.renderPage(w, "account_login", page)
			return
		}

		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form body", http.StatusBadRequest)
			return
		}
		page.RedirectTo = storefrontSafeRedirectPath(r.FormValue("redirect_to"), "/account/orders")
		page.Email = strings.TrimSpace(r.FormValue("email"))
		out, err := h.auth.Login(r.Context(), appAuth.LoginInput{
			Email:    page.Email,
			Password: r.FormValue("password"),
		})
		if err != nil {
			page.ErrorMessage = storefrontAccountErrorMessage(err)
			h.renderPageStatus(w, "account_login", page, storefrontAccountErrorStatus(err))
			return
		}
		storefrontSetSessionCookie(w, r, out.Token, out.ExpiresAt)
		http.Redirect(w, r, page.RedirectTo, http.StatusSeeOther)
	}
}

func (h *StorefrontHandler) Register() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.auth == nil || !h.engine.HasTemplate("account_register") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		if storefrontCustomerID(r) != "" {
			http.Redirect(w, r, "/account/orders", http.StatusSeeOther)
			return
		}

		page := StorefrontAccountRegisterPageData{
			Layout:     h.layoutDataBestEffort(r),
			Theme:      h.engine.Theme(),
			CSRFToken:  shopandaCSRFToken(r),
			RedirectTo: "/account/orders",
		}
		if r.Method == http.MethodGet {
			page.RedirectTo = storefrontSafeRedirectPath(r.URL.Query().Get("redirect_to"), "/account/orders")
			if r.URL.Query().Get("account_deleted") == "1" {
				page.SuccessMessage = "Account deleted. You can create a new one any time."
			}
			h.renderPage(w, "account_register", page)
			return
		}

		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form body", http.StatusBadRequest)
			return
		}
		page.RedirectTo = storefrontSafeRedirectPath(r.FormValue("redirect_to"), "/account/orders")
		page.FirstName = strings.TrimSpace(r.FormValue("first_name"))
		page.LastName = strings.TrimSpace(r.FormValue("last_name"))
		page.Email = strings.TrimSpace(r.FormValue("email"))
		out, err := h.auth.Register(r.Context(), appAuth.RegisterInput{
			Email:     page.Email,
			Password:  r.FormValue("password"),
			FirstName: page.FirstName,
			LastName:  page.LastName,
		})
		if err != nil {
			page.ErrorMessage = storefrontAccountErrorMessage(err)
			h.renderPageStatus(w, "account_register", page, storefrontAccountErrorStatus(err))
			return
		}
		storefrontSetSessionCookie(w, r, out.Token, out.ExpiresAt)
		http.Redirect(w, r, page.RedirectTo, http.StatusSeeOther)
	}
}

func (h *StorefrontHandler) Logout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.auth == nil {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		if customerID := storefrontCustomerID(r); customerID != "" {
			_ = h.auth.Logout(r.Context(), customerID)
		}
		storefrontClearSessionCookie(w, r)
		http.Redirect(w, r, "/account/login?logged_out=1", http.StatusSeeOther)
	}
}

func (h *StorefrontHandler) AccountOrders() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.orders == nil || !h.engine.HasTemplate("account_orders") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		customerID, ok := h.requireStorefrontAccount(w, r)
		if !ok {
			return
		}
		orders, err := h.orders.FindByCustomerID(r.Context(), customerID)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		rows := make([]StorefrontAccountOrderRow, 0, len(orders))
		for i := range orders {
			rows = append(rows, storefrontAccountOrderRow(&orders[i]))
		}
		h.renderPage(w, "account_orders", StorefrontAccountOrdersPageData{
			Layout:       h.layoutDataBestEffort(r),
			Theme:        h.engine.Theme(),
			Orders:       rows,
			EmptyMessage: "You have not placed any orders yet.",
		})
	}
}

func (h *StorefrontHandler) AccountOrderDetail() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.orders == nil || !h.engine.HasTemplate("account_order_detail") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		customerID, ok := h.requireStorefrontAccount(w, r)
		if !ok {
			return
		}
		orderID := strings.TrimSpace(r.PathValue("orderId"))
		if orderID == "" {
			http.NotFound(w, r)
			return
		}
		o, err := h.orders.FindByID(r.Context(), orderID)
		if err != nil || o == nil || o.CustomerID != customerID {
			http.NotFound(w, r)
			return
		}
		items := make([]StorefrontAccountOrderItem, 0, len(o.Items()))
		for _, item := range o.Items() {
			lineTotal, err := item.LineTotal()
			if err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			items = append(items, StorefrontAccountOrderItem{
				Name:          item.Name,
				SKU:           item.SKU,
				Quantity:      item.Quantity,
				UnitPriceText: formatStorefrontMoney(item.UnitPrice.Amount(), item.UnitPrice.Currency()),
				LineTotalText: formatStorefrontMoney(lineTotal.Amount(), lineTotal.Currency()),
			})
		}
		h.renderPage(w, "account_order_detail", StorefrontAccountOrderDetailPageData{
			Layout:    h.layoutDataBestEffort(r),
			Theme:     h.engine.Theme(),
			OrderID:   o.ID,
			DateText:  o.CreatedAt.UTC().Format("2006-01-02"),
			Status:    storefrontAccountOrderStatus(o.Status()),
			TotalText: formatStorefrontMoney(o.TotalAmount.Amount(), o.TotalAmount.Currency()),
			Items:     items,
			BackURL:   "/account/orders",
		})
	}
}

func (h *StorefrontHandler) AccountProfile() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.auth == nil || !h.engine.HasTemplate("account_profile") {
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
		page := storefrontAccountProfilePage(h, r, profile)
		if r.Method == http.MethodGet {
			if r.URL.Query().Get("updated") == "1" {
				page.SuccessMessage = "Profile updated."
			}
			h.renderPage(w, "account_profile", page)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form body", http.StatusBadRequest)
			return
		}
		if _, err := h.auth.UpdateProfile(r.Context(), appAuth.UpdateProfileInput{
			CustomerID: customerID,
			FirstName:  r.FormValue("first_name"),
			LastName:   r.FormValue("last_name"),
		}); err != nil {
			page.ProfileErrorMessage = storefrontAccountErrorMessage(err)
			h.renderPageStatus(w, "account_profile", page, storefrontAccountErrorStatus(err))
			return
		}
		http.Redirect(w, r, "/account/profile?updated=1", http.StatusSeeOther)
	}
}

func (h *StorefrontHandler) AccountPassword() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.auth == nil || !h.engine.HasTemplate("account_profile") {
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
		profile, err := h.auth.Me(r.Context(), customerID)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		page := storefrontAccountProfilePage(h, r, profile)
		err = h.auth.ChangePassword(r.Context(), appAuth.ChangePasswordInput{
			CustomerID:      customerID,
			CurrentPassword: r.FormValue("current_password"),
			NewPassword:     r.FormValue("new_password"),
		})
		if err != nil {
			page.PasswordErrorMessage = storefrontAccountErrorMessage(err)
			h.renderPageStatus(w, "account_profile", page, storefrontAccountErrorStatus(err))
			return
		}
		storefrontClearSessionCookie(w, r)
		http.Redirect(w, r, "/account/login?password_changed=1", http.StatusSeeOther)
	}
}

func (h *StorefrontHandler) AccountDelete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.account == nil || h.auth == nil || !h.engine.HasTemplate("account_profile") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		customerID, ok := h.requireStorefrontAccount(w, r)
		if !ok {
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form body", http.StatusBadRequest)
			return
		}
		profile, err := h.auth.Me(r.Context(), customerID)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		page := storefrontAccountProfilePage(h, r, profile)
		if !strings.EqualFold(strings.TrimSpace(r.FormValue("confirm_delete")), "delete") {
			page.DeleteErrorMessage = "Type DELETE to confirm account removal."
			h.renderPageStatus(w, "account_profile", page, http.StatusUnprocessableEntity)
			return
		}
		if err := h.account.DeleteAccount(r.Context(), customerID); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		storefrontClearSessionCookie(w, r)
		http.Redirect(w, r, "/account/register?account_deleted=1", http.StatusSeeOther)
	}
}

func storefrontSessionToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	cookie, err := r.Cookie(storefrontSessionCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

func storefrontSetSessionCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
	maxAge := storefrontSessionCookieMaxAge
	if !expiresAt.IsZero() {
		seconds := int(time.Until(expiresAt).Seconds())
		if seconds > 0 {
			maxAge = seconds
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     storefrontSessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   isRequestSecure(r, nil),
		SameSite: http.SameSiteLaxMode,
	})
}

func storefrontClearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     storefrontSessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   isRequestSecure(r, nil),
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *StorefrontHandler) requireStorefrontAccount(w http.ResponseWriter, r *http.Request) (string, bool) {
	if storefrontCustomerID(r) == "" {
		redirectTo := storefrontSafeRedirectPath(r.URL.RequestURI(), "/account/orders")
		http.Redirect(w, r, "/account/login?redirect_to="+url.QueryEscape(redirectTo), http.StatusSeeOther)
		return "", false
	}
	return storefrontCustomerID(r), true
}

func storefrontAccountProfilePage(h *StorefrontHandler, r *http.Request, profile *customer.Customer) StorefrontAccountProfilePageData {
	return StorefrontAccountProfilePageData{
		Layout:    h.layoutDataBestEffort(r),
		Theme:     h.engine.Theme(),
		CSRFToken: shopandaCSRFToken(r),
		Email:     profile.Email,
		FirstName: profile.FirstName,
		LastName:  profile.LastName,
		OrdersURL: "/account/orders",
	}
}

func storefrontAccountOrderRow(o *order.Order) StorefrontAccountOrderRow {
	return StorefrontAccountOrderRow{
		ID:        o.ID,
		DateText:  o.CreatedAt.UTC().Format("2006-01-02"),
		TotalText: formatStorefrontMoney(o.TotalAmount.Amount(), o.TotalAmount.Currency()),
		Status:    storefrontAccountOrderStatus(o.Status()),
		URL:       "/account/orders/" + o.ID,
	}
}

func storefrontAccountOrderStatus(status order.OrderStatus) string {
	return strings.ToUpper(string(status[:1])) + string(status[1:])
}

func storefrontAccountErrorMessage(err error) string {
	if apperror.Is(err, apperror.CodeValidation) || apperror.Is(err, apperror.CodeUnauthorized) || apperror.Is(err, apperror.CodeConflict) || apperror.Is(err, apperror.CodeForbidden) || apperror.Is(err, apperror.CodeNotFound) {
		return err.Error()
	}
	return "Sorry, something went wrong. Please try again later."
}

func storefrontAccountErrorStatus(err error) int {
	switch {
	case apperror.Is(err, apperror.CodeUnauthorized):
		return http.StatusUnauthorized
	case apperror.Is(err, apperror.CodeForbidden):
		return http.StatusForbidden
	case apperror.Is(err, apperror.CodeNotFound):
		return http.StatusNotFound
	case apperror.Is(err, apperror.CodeConflict), apperror.Is(err, apperror.CodeValidation):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}
