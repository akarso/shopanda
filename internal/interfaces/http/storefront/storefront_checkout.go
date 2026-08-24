package storefront

import (
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	checkoutApp "github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/theme"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type StorefrontCheckoutProgressStep struct {
	Label    string
	URL      string
	Current  bool
	Complete bool
}

type StorefrontCheckoutOption struct {
	Value    string
	Label    string
	Selected bool
}

type StorefrontCheckoutAddress struct {
	FirstName string
	LastName  string
	Street    string
	City      string
	Postcode  string
	Country   string
}

type StorefrontCheckoutRate struct {
	Method   string
	Label    string
	CostText string
	Selected bool
}

type StorefrontCheckoutPayment struct {
	Method       string
	Label        string
	IsManual     bool
	IsStripe     bool
	Instructions []string
	Selected     bool
}

type StorefrontCheckoutConfirmation struct {
	OrderID      string
	Status       string
	TotalText    string
	Notice       string
	ViewOrderURL string // empty for guest orders, which have no account order page
	ContinueURL  string
	GuestEmail   string // set for guest orders; confirmation is sent here
}

type StorefrontCheckoutPageData struct {
	Layout         StorefrontLayoutData
	Theme          theme.Theme
	Progress       []StorefrontCheckoutProgressStep
	Items          []StorefrontCartItem
	Summary        StorefrontCartSummary
	Address        StorefrontCheckoutAddress
	ContactEmail   string
	Countries      []StorefrontCheckoutOption
	Rates          []StorefrontCheckoutRate
	SelectedRate   *StorefrontCheckoutRate
	Payment        StorefrontCheckoutPayment
	PaymentMethods []StorefrontCheckoutPayment
	Confirmation   *StorefrontCheckoutConfirmation
	CSRFToken      string
	ErrorMessage   string
	StripePending  bool
	PrimaryAction  string
	SecondaryURL   string
	SecondaryLabel string
}

type storefrontCheckoutResumeState struct {
	Step           string
	Address        StorefrontCheckoutAddress
	ShippingMethod string
	PaymentMethod  string
}

const storefrontCheckoutResumeQueryParam = "checkout_resume"

var storefrontCheckoutCountries = []StorefrontCheckoutOption{
	{Value: "DE", Label: "Germany"},
	{Value: "FR", Label: "France"},
	{Value: "IT", Label: "Italy"},
	{Value: "NL", Label: "Netherlands"},
	{Value: "ES", Label: "Spain"},
	{Value: "US", Label: "United States"},
}

func (h *StorefrontHandler) CheckoutAddress() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.engine.HasTemplate("checkout_address") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		currentCart, err := h.requireCheckoutCart(r)
		if err != nil {
			http.Redirect(w, r, "/cart", http.StatusSeeOther)
			return
		}
		page, err := h.buildCheckoutPageData(r, currentCart, "address")
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		customerID := storefrontCustomerID(r)
		if customerID != "" && h.checkoutVerifiedEmailGateEnabled() {
			if !h.requireStorefrontVerifiedEmail(w, r, customerID, h.checkoutVerificationRedirectTarget(r, customerID)) {
				return
			}
		}
		if customerID != "" {
			if h.renderCheckoutResume(w, r, currentCart, customerID, page) {
				return
			}
			h.prefillCheckoutFromDefaultAddress(r, customerID, &page)
		}
		page.PrimaryAction = "/checkout/shipping"
		page.SecondaryURL = "/cart"
		page.SecondaryLabel = "Back to cart"
		h.renderPage(w, "checkout_address", page)
	}
}

func (h *StorefrontHandler) CheckoutShipping() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			http.Redirect(w, r, "/checkout/address", http.StatusSeeOther)
			return
		}
		if !h.engine.HasTemplate("checkout_shipping") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		currentCart, page, ok := h.checkoutAddressPageFromPost(w, r)
		if !ok {
			return
		}
		rates, err := h.checkoutRates(r, currentCart)
		if err != nil {
			page.ErrorMessage = "No shipping rates are available for this cart right now."
			h.renderPageStatus(w, "checkout_address", page, http.StatusUnprocessableEntity)
			return
		}
		page.Progress = storefrontCheckoutProgress("shipping")
		page.Rates = rates
		page.SelectedRate = storefrontFindCheckoutRate(rates, strings.TrimSpace(r.FormValue("shipping_method")))
		page.PrimaryAction = "/checkout/payment"
		page.SecondaryURL = "/checkout/address"
		page.SecondaryLabel = "Edit address"
		h.renderPage(w, "checkout_shipping", page)
	}
}

func (h *StorefrontHandler) CheckoutPayment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			http.Redirect(w, r, "/checkout/address", http.StatusSeeOther)
			return
		}
		if !h.engine.HasTemplate("checkout_payment") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		currentCart, page, ok := h.checkoutAddressPageFromPost(w, r)
		if !ok {
			return
		}
		rates, err := h.checkoutRates(r, currentCart)
		if err != nil {
			page.ErrorMessage = "No shipping rates are available for this cart right now."
			h.renderPageStatus(w, "checkout_address", page, http.StatusUnprocessableEntity)
			return
		}
		selected := storefrontFindCheckoutRate(rates, strings.TrimSpace(r.FormValue("shipping_method")))
		if selected == nil {
			page.Progress = storefrontCheckoutProgress("shipping")
			page.Rates = rates
			page.ErrorMessage = "Select a shipping method to continue."
			page.PrimaryAction = "/checkout/payment"
			page.SecondaryURL = "/checkout/address"
			page.SecondaryLabel = "Edit address"
			h.renderPageStatus(w, "checkout_shipping", page, http.StatusUnprocessableEntity)
			return
		}
		page.Progress = storefrontCheckoutProgress("payment")
		page.Rates = rates
		page.SelectedRate = selected
		page.PaymentMethods = storefrontCheckoutPaymentMethods(h.payments)
		if len(page.PaymentMethods) == 1 {
			page.Payment = page.PaymentMethods[0]
		}
		page.PrimaryAction = "/checkout/confirm"
		page.SecondaryURL = "/checkout/address"
		page.SecondaryLabel = "Start over"
		h.renderPage(w, "checkout_payment", page)
	}
}

func (h *StorefrontHandler) CheckoutConfirm() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			http.Redirect(w, r, "/checkout/address", http.StatusSeeOther)
			return
		}
		if !h.engine.HasTemplate("checkout_confirm") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		currentCart, page, ok := h.checkoutAddressPageFromPost(w, r)
		if !ok {
			return
		}
		rates, err := h.checkoutRates(r, currentCart)
		if err != nil {
			page.ErrorMessage = "No shipping rates are available for this cart right now."
			h.renderPageStatus(w, "checkout_address", page, http.StatusUnprocessableEntity)
			return
		}
		selectedRate := storefrontFindCheckoutRate(rates, strings.TrimSpace(r.FormValue("shipping_method")))
		if selectedRate == nil {
			page.Progress = storefrontCheckoutProgress("shipping")
			page.Rates = rates
			page.ErrorMessage = "Select a shipping method to continue."
			page.PrimaryAction = "/checkout/payment"
			page.SecondaryURL = "/checkout/address"
			page.SecondaryLabel = "Edit address"
			h.renderPageStatus(w, "checkout_shipping", page, http.StatusUnprocessableEntity)
			return
		}
		page.Progress = storefrontCheckoutProgress("payment")
		page.Rates = rates
		page.SelectedRate = selectedRate
		page.PaymentMethods = storefrontCheckoutPaymentMethods(h.payments)
		paymentMethod := strings.TrimSpace(r.FormValue("payment_method"))
		selectedPayment := storefrontFindCheckoutPayment(page.PaymentMethods, paymentMethod)
		if selectedPayment == nil {
			if len(page.PaymentMethods) == 1 {
				selectedPayment = &page.PaymentMethods[0]
				paymentMethod = selectedPayment.Method
			}
		}
		if selectedPayment == nil {
			page.ErrorMessage = "Select a valid payment method to continue."
			page.PrimaryAction = "/checkout/confirm"
			page.SecondaryURL = "/checkout/address"
			page.SecondaryLabel = "Start over"
			h.renderPageStatus(w, "checkout_payment", page, http.StatusUnprocessableEntity)
			return
		}
		page.Payment = *selectedPayment
		if h.checkout == nil {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		customerID := storefrontCustomerID(r)
		cctx, err := h.checkout.StartCheckout(r.Context(), currentCart.ID, customerID, checkoutApp.Input{
			Address: checkoutApp.Address{
				FirstName: page.Address.FirstName,
				LastName:  page.Address.LastName,
				Street:    page.Address.Street,
				City:      page.Address.City,
				Postcode:  page.Address.Postcode,
				Country:   page.Address.Country,
			},
			ContactEmail:   page.ContactEmail,
			ShippingMethod: selectedRate.Method,
			PaymentMethod:  paymentMethod,
		})
		if err != nil {
			page.ErrorMessage = storefrontCheckoutErrorMessage(err)
			page.PrimaryAction = "/checkout/confirm"
			page.SecondaryURL = "/checkout/address"
			page.SecondaryLabel = "Start over"
			h.renderPageStatus(w, "checkout_payment", page, storefrontCheckoutErrorStatus(err))
			return
		}
		page.Progress = storefrontCheckoutProgress("confirm")
		page.Confirmation = &StorefrontCheckoutConfirmation{
			OrderID:     cctx.Order.ID,
			Status:      string(cctx.Order.Status()),
			TotalText:   storefrontCheckoutDisplayTotal(cctx, selectedRate.CostText),
			Notice:      storefrontCheckoutConfirmationNotice(payment.PaymentMethod(paymentMethod)),
			ContinueURL: "/products",
		}
		if customerID == "" {
			page.Confirmation.GuestEmail = cctx.Order.ContactEmail
		} else {
			page.Confirmation.ViewOrderURL = "/account/orders/" + cctx.Order.ID
		}
		page.StripePending = paymentMethod == string(payment.MethodStripe)
		h.renderPage(w, "checkout_confirm", page)
	}
}

func (h *StorefrontHandler) checkoutAddressPageFromPost(w http.ResponseWriter, r *http.Request) (*cart.Cart, StorefrontCheckoutPageData, bool) {
	currentCart, err := h.requireCheckoutCart(r)
	if err != nil {
		http.Redirect(w, r, "/cart", http.StatusSeeOther)
		return nil, StorefrontCheckoutPageData{}, false
	}
	page, err := h.buildCheckoutPageData(r, currentCart, "address")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return nil, StorefrontCheckoutPageData{}, false
	}
	customerID := storefrontCustomerID(r)
	if customerID != "" && h.checkoutVerifiedEmailGateEnabled() {
		if !h.requireStorefrontVerifiedEmail(w, r, customerID, h.checkoutVerificationRedirectTarget(r, customerID)) {
			return nil, StorefrontCheckoutPageData{}, false
		}
	}
	page.Address = storefrontCheckoutAddressFromRequest(r)
	page.ContactEmail = storefrontCheckoutContactEmailFromRequest(r)
	page.Countries = storefrontCheckoutCountryOptions(page.Address.Country)
	if err := page.Address.Validate(); err != nil {
		page.ErrorMessage = err.Error()
		page.PrimaryAction = "/checkout/shipping"
		page.SecondaryURL = "/cart"
		page.SecondaryLabel = "Back to cart"
		h.renderPageStatus(w, "checkout_address", page, http.StatusUnprocessableEntity)
		return nil, StorefrontCheckoutPageData{}, false
	}
	if strings.TrimSpace(customerID) == "" {
		if err := storefrontCheckoutContactEmailValidate(page.ContactEmail); err != nil {
			page.ErrorMessage = err.Error()
			page.PrimaryAction = "/checkout/shipping"
			page.SecondaryURL = "/cart"
			page.SecondaryLabel = "Back to cart"
			h.renderPageStatus(w, "checkout_address", page, http.StatusUnprocessableEntity)
			return nil, StorefrontCheckoutPageData{}, false
		}
	}
	return currentCart, page, true
}

// prefillCheckoutFromDefaultAddress best-effort populates the checkout address
// form from the authenticated customer's saved default address. It never blocks
// checkout: any lookup failure leaves the form empty for manual entry.
func (h *StorefrontHandler) prefillCheckoutFromDefaultAddress(r *http.Request, customerID string, page *StorefrontCheckoutPageData) {
	if h.addresses == nil {
		return
	}
	addr, err := h.addresses.FindDefault(r.Context(), customerID)
	if err != nil || addr == nil {
		return
	}
	firstName, lastName := storefrontSplitRecipient(addr.Recipient)
	page.Address = StorefrontCheckoutAddress{
		FirstName: firstName,
		LastName:  lastName,
		Street:    addr.Street,
		City:      addr.City,
		Postcode:  addr.Postcode,
		Country:   addr.Country,
	}
	page.Countries = storefrontCheckoutCountryOptions(addr.Country)
	if strings.TrimSpace(page.ContactEmail) == "" && h.auth != nil {
		if profile, err := h.auth.Me(r.Context(), customerID); err == nil && profile != nil {
			page.ContactEmail = profile.Email
		}
	}
}

func storefrontSplitRecipient(recipient string) (string, string) {
	recipient = strings.TrimSpace(recipient)
	if recipient == "" {
		return "", ""
	}
	parts := strings.SplitN(recipient, " ", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func (h *StorefrontHandler) checkoutVerifiedEmailGateEnabled() bool {
	return h.auth != nil && h.security != nil && strings.TrimSpace(h.security.storeBaseURL) != ""
}

func storefrontCheckoutResumeStep(step string) string {
	switch strings.TrimSpace(step) {
	case "shipping":
		return "shipping"
	case "payment":
		return "payment"
	default:
		return "address"
	}
}

func storefrontCheckoutResumeStateFromRequest(r *http.Request) (storefrontCheckoutResumeState, bool) {
	if r == nil {
		return storefrontCheckoutResumeState{}, false
	}
	state := storefrontCheckoutResumeState{
		Address:        storefrontCheckoutAddressFromRequest(r),
		ShippingMethod: strings.TrimSpace(r.FormValue("shipping_method")),
		PaymentMethod:  strings.TrimSpace(r.FormValue("payment_method")),
	}
	switch r.URL.Path {
	case "/checkout/shipping":
		state.Step = "shipping"
	case "/checkout/payment", "/checkout/confirm":
		state.Step = "payment"
	default:
		return storefrontCheckoutResumeState{}, false
	}
	return state, true
}

func (h *StorefrontHandler) checkoutVerificationRedirectTarget(r *http.Request, customerID string) string {
	if strings.TrimSpace(customerID) == "" || h.security == nil {
		return "/checkout/address"
	}
	state, ok := storefrontCheckoutResumeStateFromRequest(r)
	if !ok {
		return "/checkout/address"
	}
	token, err := h.security.checkoutResumeToken(customerID, state, time.Now().UTC())
	if err != nil {
		h.log.Error("storefront.checkout.resume_token_failed", err, map[string]interface{}{
			"customer_id": customerID,
			"path":        r.URL.Path,
		})
		return "/checkout/address"
	}
	query := url.Values{}
	query.Set(storefrontCheckoutResumeQueryParam, token)
	return "/checkout/address?" + query.Encode()
}

func (h *StorefrontHandler) renderCheckoutResume(w http.ResponseWriter, r *http.Request, currentCart *cart.Cart, customerID string, page StorefrontCheckoutPageData) bool {
	if h.security == nil || strings.TrimSpace(customerID) == "" {
		return false
	}
	token := strings.TrimSpace(r.URL.Query().Get(storefrontCheckoutResumeQueryParam))
	if token == "" {
		return false
	}
	state, ok := h.security.verifyCheckoutResumeToken(token, customerID)
	if !ok {
		return false
	}
	page.Address = state.Address
	page.Countries = storefrontCheckoutCountryOptions(page.Address.Country)
	if err := page.Address.Validate(); err != nil {
		return false
	}
	rates, err := h.checkoutRates(r, currentCart)
	if err != nil {
		return false
	}
	switch storefrontCheckoutResumeStep(state.Step) {
	case "shipping":
		page.Progress = storefrontCheckoutProgress("shipping")
		page.Rates = rates
		page.SelectedRate = storefrontFindCheckoutRate(rates, state.ShippingMethod)
		page.PrimaryAction = "/checkout/payment"
		page.SecondaryURL = "/checkout/address"
		page.SecondaryLabel = "Edit address"
		h.renderPage(w, "checkout_shipping", page)
		return true
	case "payment":
		selectedRate := storefrontFindCheckoutRate(rates, state.ShippingMethod)
		if selectedRate == nil {
			return false
		}
		page.Progress = storefrontCheckoutProgress("payment")
		page.Rates = rates
		page.SelectedRate = selectedRate
		page.PaymentMethods = storefrontCheckoutPaymentMethods(h.payments)
		selectedPayment := storefrontFindCheckoutPayment(page.PaymentMethods, strings.TrimSpace(state.PaymentMethod))
		if selectedPayment == nil {
			if len(page.PaymentMethods) == 1 {
				selectedPayment = &page.PaymentMethods[0]
			}
		}
		if selectedPayment == nil {
			return false
		}
		page.Payment = *selectedPayment
		page.PrimaryAction = "/checkout/confirm"
		page.SecondaryURL = "/checkout/address"
		page.SecondaryLabel = "Start over"
		h.renderPage(w, "checkout_payment", page)
		return true
	default:
		return false
	}
}

func (h *StorefrontHandler) buildCheckoutPageData(r *http.Request, currentCart *cart.Cart, step string) (StorefrontCheckoutPageData, error) {
	layout := h.layoutDataBestEffort(r)
	cartPage, err := h.buildCartPageData(r, layout, currentCart)
	if err != nil {
		return StorefrontCheckoutPageData{}, err
	}
	return StorefrontCheckoutPageData{
		Layout:         layout,
		Theme:          h.engine.Theme(),
		Progress:       storefrontCheckoutProgress(step),
		Items:          cartPage.Items,
		Summary:        cartPage.Summary,
		Countries:      storefrontCheckoutCountryOptions(""),
		CSRFToken:      httpshared.CSRFToken(r),
		PrimaryAction:  "/checkout/shipping",
		SecondaryURL:   "/cart",
		SecondaryLabel: "Back to cart",
	}, nil
}

func (h *StorefrontHandler) requireCheckoutCart(r *http.Request) (*cart.Cart, error) {
	currentCart, err := h.currentCart(r)
	if err != nil {
		return nil, err
	}
	if currentCart == nil || len(currentCart.Items) == 0 {
		return nil, fmt.Errorf("checkout cart is empty")
	}
	return currentCart, nil
}

func (h *StorefrontHandler) checkoutRates(r *http.Request, currentCart *cart.Cart) ([]StorefrontCheckoutRate, error) {
	if len(h.shipping) == 0 {
		return nil, fmt.Errorf("no shipping providers configured")
	}
	rates := make([]StorefrontCheckoutRate, 0, len(h.shipping))
	for _, provider := range h.shipping {
		rate, err := provider.CalculateRate(r.Context(), currentCart.ID, currentCart.Currency, currentCart.TotalQuantity())
		if err != nil {
			continue
		}
		rates = append(rates, StorefrontCheckoutRate{
			Method:   string(provider.Method()),
			Label:    rate.Label,
			CostText: formatStorefrontMoney(rate.Cost.Amount(), rate.Cost.Currency()),
		})
	}
	if len(rates) == 0 {
		return nil, fmt.Errorf("no shipping rates available")
	}
	if len(rates) == 1 {
		rates[0].Selected = true
	}
	return rates, nil
}

func storefrontCheckoutAddressFromRequest(r *http.Request) StorefrontCheckoutAddress {
	return StorefrontCheckoutAddress{
		FirstName: strings.TrimSpace(r.FormValue("first_name")),
		LastName:  strings.TrimSpace(r.FormValue("last_name")),
		Street:    strings.TrimSpace(r.FormValue("street")),
		City:      strings.TrimSpace(r.FormValue("city")),
		Postcode:  strings.TrimSpace(r.FormValue("postcode")),
		Country:   strings.TrimSpace(r.FormValue("country")),
	}
}

func storefrontCheckoutContactEmailFromRequest(r *http.Request) string {
	return strings.ToLower(strings.TrimSpace(r.FormValue("contact_email")))
}

func storefrontCheckoutContactEmailValidate(email string) error {
	if email == "" {
		return fmt.Errorf("Contact email is required.")
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return fmt.Errorf("Contact email is invalid.")
	}
	return nil
}

func (a StorefrontCheckoutAddress) Validate() error {
	switch {
	case a.FirstName == "":
		return fmt.Errorf("First name is required.")
	case a.LastName == "":
		return fmt.Errorf("Last name is required.")
	case a.Street == "":
		return fmt.Errorf("Street is required.")
	case a.City == "":
		return fmt.Errorf("City is required.")
	case a.Postcode == "":
		return fmt.Errorf("Postcode is required.")
	case a.Country == "":
		return fmt.Errorf("Country is required.")
	default:
		return nil
	}
}

func storefrontCheckoutProgress(step string) []StorefrontCheckoutProgressStep {
	steps := []StorefrontCheckoutProgressStep{
		{Label: "Address", URL: "/checkout/address"},
		{Label: "Shipping", URL: "/checkout/shipping"},
		{Label: "Payment", URL: "/checkout/payment"},
		{Label: "Confirm", URL: "/checkout/confirm"},
	}
	current := 0
	for i, candidate := range []string{"address", "shipping", "payment", "confirm"} {
		if candidate == step {
			current = i
			break
		}
	}
	for i := range steps {
		steps[i].Current = i == current
		steps[i].Complete = i < current
	}
	return steps
}

func storefrontCheckoutCountryOptions(selected string) []StorefrontCheckoutOption {
	options := make([]StorefrontCheckoutOption, len(storefrontCheckoutCountries))
	copy(options, storefrontCheckoutCountries)
	for i := range options {
		options[i].Selected = options[i].Value == selected
	}
	return options
}

func storefrontFindCheckoutRate(rates []StorefrontCheckoutRate, method string) *StorefrontCheckoutRate {
	if len(rates) == 0 {
		return nil
	}
	for i := range rates {
		rates[i].Selected = rates[i].Method == method || (method == "" && i == 0)
		if rates[i].Selected {
			return &rates[i]
		}
	}
	return nil
}

func storefrontCheckoutPaymentMethods(registry *payment.ProviderRegistry) []StorefrontCheckoutPayment {
	if registry == nil || registry.Len() == 0 {
		return []StorefrontCheckoutPayment{storefrontCheckoutPaymentView(nil)}
	}
	methods := registry.Methods()
	views := make([]StorefrontCheckoutPayment, 0, len(methods))
	for i, method := range methods {
		provider, ok := registry.Get(method)
		if !ok {
			continue
		}
		view := storefrontCheckoutPaymentView(provider)
		view.Selected = i == 0 && len(methods) == 1
		views = append(views, view)
	}
	if len(views) == 0 {
		return []StorefrontCheckoutPayment{storefrontCheckoutPaymentView(nil)}
	}
	return views
}

func storefrontFindCheckoutPayment(methods []StorefrontCheckoutPayment, method string) *StorefrontCheckoutPayment {
	if len(methods) == 0 {
		return nil
	}
	method = strings.TrimSpace(method)
	if method == "" {
		return nil
	}
	for i := range methods {
		methods[i].Selected = methods[i].Method == method
		if methods[i].Selected {
			return &methods[i]
		}
	}
	return nil
}

func storefrontCheckoutPaymentView(provider payment.Provider) StorefrontCheckoutPayment {
	view := StorefrontCheckoutPayment{Method: "manual", Label: "Manual payment"}
	if provider == nil {
		view.IsManual = true
		view.Instructions = []string{
			"Place the order to receive bank transfer instructions on the confirmation page.",
			"Orders stay server-rendered and work without client-side orchestration.",
		}
		return view
	}
	view.Method = string(provider.Method())
	switch provider.Method() {
	case payment.MethodStripe:
		view.Label = "Stripe"
		view.IsStripe = true
		view.Instructions = []string{
			"Stripe creates a payment intent during order placement.",
			"Card confirmation continues with Stripe after the order has been created.",
		}
	default:
		view.IsManual = true
		view.Instructions = []string{
			"Place the order to receive bank transfer instructions on the confirmation page.",
			"Orders stay server-rendered and work without client-side orchestration.",
		}
	}
	return view
}

func storefrontCheckoutConfirmationNotice(method payment.PaymentMethod) string {
	if method == payment.MethodStripe {
		return "Your order is created and Stripe payment confirmation is still required."
	}
	return "Your order has been placed and manual payment instructions are ready."
}

func storefrontCheckoutDisplayTotal(cctx *checkoutApp.Context, shippingCostText string) string {
	if cctx == nil || cctx.Order == nil {
		return shippingCostText
	}
	return formatStorefrontMoney(cctx.Order.TotalAmount.Amount(), cctx.Order.TotalAmount.Currency())
}

func storefrontCheckoutErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	if storefrontCheckoutErrorStatus(err) >= http.StatusInternalServerError {
		return "Sorry, something went wrong. Please try again later."
	}
	return err.Error()
}

func storefrontCheckoutErrorStatus(err error) int {
	switch {
	case apperror.Is(err, apperror.CodeValidation):
		return http.StatusUnprocessableEntity
	case apperror.Is(err, apperror.CodeNotFound):
		return http.StatusNotFound
	case apperror.Is(err, apperror.CodeForbidden):
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}
