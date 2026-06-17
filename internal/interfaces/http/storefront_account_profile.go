package http

import (
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/legal"
	"github.com/akarso/shopanda/internal/domain/theme"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

// StorefrontAccountAddressForm holds the editable fields of a saved address.
type StorefrontAccountAddressForm struct {
	ID        string
	Label     string
	Recipient string
	Street    string
	City      string
	Postcode  string
	Country   string
	IsDefault bool
}

// StorefrontAccountAddressRow is a saved address rendered in the list.
type StorefrontAccountAddressRow struct {
	ID        string
	Label     string
	Recipient string
	Lines     []string
	IsDefault bool
	EditURL   string
}

// StorefrontAccountAddressesPageData drives the saved-addresses page.
type StorefrontAccountAddressesPageData struct {
	Layout         StorefrontLayoutData
	Theme          theme.Theme
	AccountNav     StorefrontAccountNavData
	CSRFToken      string
	Addresses      []StorefrontAccountAddressRow
	Countries      []StorefrontCheckoutOption
	Form           StorefrontAccountAddressForm
	FormAction     string
	FormTitle      string
	SubmitLabel    string
	Editing        bool
	EmptyMessage   string
	ErrorMessage   string
	SuccessMessage string
}

// StorefrontAccountPreferencesPageData drives the marketing preferences page.
type StorefrontAccountPreferencesPageData struct {
	Layout         StorefrontLayoutData
	Theme          theme.Theme
	AccountNav     StorefrontAccountNavData
	CSRFToken      string
	Marketing      bool
	SuccessMessage string
	ErrorMessage   string
}

// AccountAddresses renders the saved address list with an add/edit form.
func (h *StorefrontHandler) AccountAddresses() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.addresses == nil || !h.engine.HasTemplate("account_addresses") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		customerID, ok := h.requireStorefrontAccount(w, r)
		if !ok {
			return
		}
		page, err := h.buildAddressesPage(r, customerID, StorefrontAccountAddressForm{}, false)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if editID := strings.TrimSpace(r.URL.Query().Get("edit")); editID != "" {
			addr, err := h.findOwnedAddress(r, customerID, editID)
			if err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			if addr == nil {
				http.NotFound(w, r)
				return
			}
			h.applyAddressEditForm(&page, addr)
		}
		switch r.URL.Query().Get("saved") {
		case "1":
			page.SuccessMessage = "Address saved."
		case "default":
			page.SuccessMessage = "Default address updated."
		case "deleted":
			page.SuccessMessage = "Address removed."
		}
		h.renderPage(w, "account_addresses", page)
	}
}

// AccountAddressCreate handles POST /account/addresses.
func (h *StorefrontHandler) AccountAddressCreate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.addresses == nil || !h.engine.HasTemplate("account_addresses") {
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
		form := storefrontAccountAddressFormFromRequest(r)
		addr, err := customer.NewAddress(id.New(), customerID, form.Label, form.Recipient, form.Street, form.City, form.Postcode, form.Country)
		if err != nil {
			h.renderAddressFormError(w, r, customerID, form, false, apperror.Validation(err.Error()))
			return
		}
		addr.IsDefault = form.IsDefault
		if err := h.addresses.Create(r.Context(), &addr); err != nil {
			h.renderAddressFormError(w, r, customerID, form, false, err)
			return
		}
		http.Redirect(w, r, "/account/addresses?saved=1", http.StatusSeeOther)
	}
}

// AccountAddressUpdate handles POST /account/addresses/{addressId}.
func (h *StorefrontHandler) AccountAddressUpdate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.addresses == nil || !h.engine.HasTemplate("account_addresses") {
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
		addressID := strings.TrimSpace(r.PathValue("addressId"))
		existing, err := h.findOwnedAddress(r, customerID, addressID)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if existing == nil {
			http.NotFound(w, r)
			return
		}
		form := storefrontAccountAddressFormFromRequest(r)
		form.ID = addressID
		if err := existing.Apply(form.Label, form.Recipient, form.Street, form.City, form.Postcode, form.Country); err != nil {
			h.renderAddressFormError(w, r, customerID, form, true, apperror.Validation(err.Error()))
			return
		}
		existing.IsDefault = existing.IsDefault || form.IsDefault
		if err := h.addresses.Update(r.Context(), existing); err != nil {
			h.renderAddressFormError(w, r, customerID, form, true, err)
			return
		}
		http.Redirect(w, r, "/account/addresses?saved=1", http.StatusSeeOther)
	}
}

// AccountAddressSetDefault handles POST /account/addresses/{addressId}/default.
func (h *StorefrontHandler) AccountAddressSetDefault() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.addresses == nil {
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
		addressID := strings.TrimSpace(r.PathValue("addressId"))
		if err := h.addresses.SetDefault(r.Context(), customerID, addressID); err != nil {
			if apperror.Is(err, apperror.CodeNotFound) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/account/addresses?saved=default", http.StatusSeeOther)
	}
}

// AccountAddressDelete handles POST /account/addresses/{addressId}/delete.
func (h *StorefrontHandler) AccountAddressDelete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.addresses == nil {
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
		addressID := strings.TrimSpace(r.PathValue("addressId"))
		if err := h.addresses.Delete(r.Context(), customerID, addressID); err != nil {
			if apperror.Is(err, apperror.CodeNotFound) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/account/addresses?saved=deleted", http.StatusSeeOther)
	}
}

// AccountPreferences renders and saves marketing consent.
func (h *StorefrontHandler) AccountPreferences() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.consents == nil || !h.engine.HasTemplate("account_preferences") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		customerID, ok := h.requireStorefrontAccount(w, r)
		if !ok {
			return
		}
		existing, err := h.consents.FindByCustomerID(r.Context(), customerID)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		page := StorefrontAccountPreferencesPageData{
			Layout:     h.layoutDataBestEffort(r),
			Theme:      h.engine.Theme(),
			AccountNav: storefrontAccountNav("preferences"),
			CSRFToken:  shopandaCSRFToken(r),
		}
		if existing != nil {
			page.Marketing = existing.Marketing
		}
		if r.Method == http.MethodGet {
			if r.URL.Query().Get("updated") == "1" {
				page.SuccessMessage = "Preferences updated."
			}
			h.renderPage(w, "account_preferences", page)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form body", http.StatusBadRequest)
			return
		}
		consent, err := legal.NewConsent(customerID)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		analytics := false
		if existing != nil {
			analytics = existing.Analytics
		}
		marketing := storefrontFormChecked(r, "marketing")
		consent.Update(analytics, marketing)
		if err := h.consents.Upsert(r.Context(), &consent); err != nil {
			page.Marketing = marketing
			page.ErrorMessage = storefrontAccountErrorMessage(err)
			h.renderPageStatus(w, "account_preferences", page, storefrontAccountErrorStatus(err))
			return
		}
		http.Redirect(w, r, "/account/preferences?updated=1", http.StatusSeeOther)
	}
}

func (h *StorefrontHandler) buildAddressesPage(r *http.Request, customerID string, form StorefrontAccountAddressForm, editing bool) (StorefrontAccountAddressesPageData, error) {
	addresses, err := h.addresses.ListByCustomer(r.Context(), customerID)
	if err != nil {
		return StorefrontAccountAddressesPageData{}, err
	}
	rows := make([]StorefrontAccountAddressRow, 0, len(addresses))
	for i := range addresses {
		rows = append(rows, storefrontAccountAddressRow(&addresses[i]))
	}
	page := StorefrontAccountAddressesPageData{
		Layout:       h.layoutDataBestEffort(r),
		Theme:        h.engine.Theme(),
		AccountNav:   storefrontAccountNav("addresses"),
		CSRFToken:    shopandaCSRFToken(r),
		Addresses:    rows,
		Countries:    storefrontCheckoutCountryOptions(form.Country),
		Form:         form,
		FormAction:   "/account/addresses",
		FormTitle:    "Add an address",
		SubmitLabel:  "Save address",
		Editing:      editing,
		EmptyMessage: "You have not saved any addresses yet.",
	}
	if editing {
		page.FormAction = "/account/addresses/" + form.ID
		page.FormTitle = "Edit address"
		page.SubmitLabel = "Update address"
	}
	return page, nil
}

func (h *StorefrontHandler) applyAddressEditForm(page *StorefrontAccountAddressesPageData, addr *customer.Address) {
	form := StorefrontAccountAddressForm{
		ID:        addr.ID,
		Label:     addr.Label,
		Recipient: addr.Recipient,
		Street:    addr.Street,
		City:      addr.City,
		Postcode:  addr.Postcode,
		Country:   addr.Country,
		IsDefault: addr.IsDefault,
	}
	page.Form = form
	page.Editing = true
	page.FormAction = "/account/addresses/" + addr.ID
	page.FormTitle = "Edit address"
	page.SubmitLabel = "Update address"
	page.Countries = storefrontCheckoutCountryOptions(form.Country)
}

func (h *StorefrontHandler) renderAddressFormError(w http.ResponseWriter, r *http.Request, customerID string, form StorefrontAccountAddressForm, editing bool, cause error) {
	page, err := h.buildAddressesPage(r, customerID, form, editing)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	page.ErrorMessage = storefrontAccountErrorMessage(cause)
	h.renderPageStatus(w, "account_addresses", page, storefrontAccountErrorStatus(cause))
}

func (h *StorefrontHandler) findOwnedAddress(r *http.Request, customerID, addressID string) (*customer.Address, error) {
	if strings.TrimSpace(addressID) == "" {
		return nil, nil
	}
	addr, err := h.addresses.FindByID(r.Context(), addressID)
	if err != nil {
		return nil, err
	}
	if addr == nil || addr.CustomerID != customerID {
		return nil, nil
	}
	return addr, nil
}

func storefrontAccountAddressRow(a *customer.Address) StorefrontAccountAddressRow {
	lines := []string{a.Street}
	cityLine := strings.TrimSpace(strings.TrimSpace(a.Postcode+" ") + a.City)
	if cityLine != "" {
		lines = append(lines, cityLine)
	}
	if country := storefrontCountryLabel(a.Country); country != "" {
		lines = append(lines, country)
	}
	return StorefrontAccountAddressRow{
		ID:        a.ID,
		Label:     a.Label,
		Recipient: a.Recipient,
		Lines:     lines,
		IsDefault: a.IsDefault,
		EditURL:   "/account/addresses?edit=" + a.ID,
	}
}

func storefrontAccountAddressFormFromRequest(r *http.Request) StorefrontAccountAddressForm {
	return StorefrontAccountAddressForm{
		Label:     strings.TrimSpace(r.FormValue("label")),
		Recipient: strings.TrimSpace(r.FormValue("recipient")),
		Street:    strings.TrimSpace(r.FormValue("street")),
		City:      strings.TrimSpace(r.FormValue("city")),
		Postcode:  strings.TrimSpace(r.FormValue("postcode")),
		Country:   strings.TrimSpace(r.FormValue("country")),
		IsDefault: storefrontFormChecked(r, "is_default"),
	}
}

func storefrontCountryLabel(code string) string {
	code = strings.TrimSpace(code)
	for _, option := range storefrontCheckoutCountries {
		if option.Value == code {
			return option.Label
		}
	}
	return code
}

func storefrontFormChecked(r *http.Request, field string) bool {
	switch strings.ToLower(strings.TrimSpace(r.FormValue(field))) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}
