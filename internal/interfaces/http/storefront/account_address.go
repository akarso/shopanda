package storefront

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
	"github.com/akarso/shopanda/internal/platform/id"
)

// AddressHandler serves JSON CRUD for saved customer addresses.
type AddressHandler struct {
	addresses customer.AddressRepository
}

// NewAddressHandler creates an AddressHandler.
func NewAddressHandler(addresses customer.AddressRepository) *AddressHandler {
	if addresses == nil {
		panic("AddressHandler: addresses repository must not be nil")
	}
	return &AddressHandler{addresses: addresses}
}

type addressJSON struct {
	ID         string `json:"id"`
	CustomerID string `json:"customer_id"`
	Label      string `json:"label"`
	Recipient  string `json:"recipient"`
	Street     string `json:"street"`
	City       string `json:"city"`
	Postcode   string `json:"postcode"`
	Country    string `json:"country"`
	IsDefault  bool   `json:"is_default"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

type addressWriteRequest struct {
	Label     string `json:"label"`
	Recipient string `json:"recipient"`
	Street    string `json:"street"`
	City      string `json:"city"`
	Postcode  string `json:"postcode"`
	Country   string `json:"country"`
	IsDefault *bool  `json:"is_default"`
}

func addressResponse(a *customer.Address) addressJSON {
	return addressJSON{
		ID:         a.ID,
		CustomerID: a.CustomerID,
		Label:      a.Label,
		Recipient:  a.Recipient,
		Street:     a.Street,
		City:       a.City,
		Postcode:   a.Postcode,
		Country:    a.Country,
		IsDefault:  a.IsDefault,
		CreatedAt:  a.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:  a.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// List handles GET /api/v1/account/addresses.
func (h *AddressHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID
		list, err := h.addresses.ListByCustomer(r.Context(), customerID)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		out := make([]addressJSON, 0, len(list))
		for i := range list {
			out = append(out, addressResponse(&list[i]))
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"addresses": out})
	}
}

// Create handles POST /api/v1/account/addresses.
func (h *AddressHandler) Create() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID
		var req addressWriteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpshared.JSONError(w, apperror.Validation("invalid JSON body"))
			return
		}
		addr, err := customer.NewAddress(id.New(), customerID, req.Label, req.Recipient, req.Street, req.City, req.Postcode, req.Country)
		if err != nil {
			httpshared.JSONError(w, apperror.Validation(err.Error()))
			return
		}
		if req.IsDefault != nil {
			addr.IsDefault = *req.IsDefault
		}
		if err := h.addresses.Create(r.Context(), &addr); err != nil {
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusCreated, map[string]interface{}{"address": addressResponse(&addr)})
	}
}

// Update handles PUT /api/v1/account/addresses/{addressId}.
func (h *AddressHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID
		addressID := strings.TrimSpace(r.PathValue("addressId"))
		existing, err := h.ownedAddress(r, customerID, addressID)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		var req addressWriteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpshared.JSONError(w, apperror.Validation("invalid JSON body"))
			return
		}
		if err := existing.Apply(req.Label, req.Recipient, req.Street, req.City, req.Postcode, req.Country); err != nil {
			httpshared.JSONError(w, apperror.Validation(err.Error()))
			return
		}
		if req.IsDefault != nil {
			existing.IsDefault = *req.IsDefault
		}
		if err := h.addresses.Update(r.Context(), existing); err != nil {
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"address": addressResponse(existing)})
	}
}

// SetDefault handles POST /api/v1/account/addresses/{addressId}/default.
func (h *AddressHandler) SetDefault() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID
		addressID := strings.TrimSpace(r.PathValue("addressId"))
		if err := h.addresses.SetDefault(r.Context(), customerID, addressID); err != nil {
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"ok": true})
	}
}

// Delete handles DELETE /api/v1/account/addresses/{addressId}.
func (h *AddressHandler) Delete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID
		addressID := strings.TrimSpace(r.PathValue("addressId"))
		if err := h.addresses.Delete(r.Context(), customerID, addressID); err != nil {
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"ok": true})
	}
}

func (h *AddressHandler) ownedAddress(r *http.Request, customerID, addressID string) (*customer.Address, error) {
	if addressID == "" {
		return nil, apperror.Validation("address id is required")
	}
	addr, err := h.addresses.FindByID(r.Context(), addressID)
	if err != nil {
		return nil, err
	}
	if addr == nil || addr.CustomerID != customerID {
		return nil, apperror.NotFound("address not found")
	}
	return addr, nil
}
