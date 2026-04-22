package http

import (
	"fmt"
	"net/http"
	"strings"

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
}

type StorefrontCheckoutConfirmation struct {
	OrderID      string
	Status       string
	TotalText    string
	Notice       string
	ViewOrderURL string
	ContinueURL  string
}

type StorefrontCheckoutPageData struct {
	Layout         StorefrontLayoutData
	Theme          theme.Theme
	Progress       []StorefrontCheckoutProgressStep
	Items          []StorefrontCartItem
	Summary        StorefrontCartSummary
	Address        StorefrontCheckoutAddress
	Countries      []StorefrontCheckoutOption
	Rates          []StorefrontCheckoutRate
	SelectedRate   *StorefrontCheckoutRate
	Payment        StorefrontCheckoutPayment
	Confirmation   *StorefrontCheckoutConfirmation
	CSRFToken      string
	ErrorMessage   string
	RequiresAuth   bool
	StripePending  bool
	PrimaryAction  string
	SecondaryURL   string
	SecondaryLabel string
}

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
		page.RequiresAuth = storefrontCustomerID(r) == ""
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
		page.Payment = storefrontCheckoutPaymentView(h.payment)
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
		page.Payment = storefrontCheckoutPaymentView(h.payment)
		paymentMethod := strings.TrimSpace(r.FormValue("payment_method"))
		if paymentMethod == "" || page.Payment.Method != paymentMethod {
			page.ErrorMessage = "Select a valid payment method to continue."
			page.PrimaryAction = "/checkout/confirm"
			page.SecondaryURL = "/checkout/address"
			page.SecondaryLabel = "Start over"
			h.renderPageStatus(w, "checkout_payment", page, http.StatusUnprocessableEntity)
			return
		}
		if page.RequiresAuth {
			page.Progress = storefrontCheckoutProgress("address")
			page.PrimaryAction = "/checkout/shipping"
			page.SecondaryURL = "/cart"
			page.SecondaryLabel = "Back to cart"
			h.renderPageStatus(w, "checkout_address", page, http.StatusUnauthorized)
			return
		}
		if h.checkout == nil {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		cctx, err := h.checkout.StartCheckout(r.Context(), currentCart.ID, storefrontCustomerID(r), checkoutApp.Input{
			Address: checkoutApp.Address{
				FirstName: page.Address.FirstName,
				LastName:  page.Address.LastName,
				Street:    page.Address.Street,
				City:      page.Address.City,
				Postcode:  page.Address.Postcode,
				Country:   page.Address.Country,
			},
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
			OrderID:      cctx.Order.ID,
			Status:       string(cctx.Order.Status()),
			TotalText:    storefrontCheckoutDisplayTotal(cctx, selectedRate.CostText),
			Notice:       storefrontCheckoutConfirmationNotice(h.payment),
			ViewOrderURL: "/api/v1/orders/" + cctx.Order.ID,
			ContinueURL:  "/products",
		}
		page.StripePending = h.payment != nil && h.payment.Method() == payment.MethodStripe
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
	page.RequiresAuth = storefrontCustomerID(r) == ""
	page.Address = storefrontCheckoutAddressFromRequest(r)
	page.Countries = storefrontCheckoutCountryOptions(page.Address.Country)
	if err := page.Address.Validate(); err != nil {
		page.ErrorMessage = err.Error()
		page.PrimaryAction = "/checkout/shipping"
		page.SecondaryURL = "/cart"
		page.SecondaryLabel = "Back to cart"
		h.renderPageStatus(w, "checkout_address", page, http.StatusUnprocessableEntity)
		return nil, StorefrontCheckoutPageData{}, false
	}
	return currentCart, page, true
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
		CSRFToken:      shopandaCSRFToken(r),
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

func storefrontCheckoutConfirmationNotice(provider payment.Provider) string {
	if provider != nil && provider.Method() == payment.MethodStripe {
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
