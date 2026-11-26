package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	storeidentity "github.com/akarso/shopanda/internal/application/storeidentity"
	"github.com/akarso/shopanda/internal/domain/store"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// StoreIdentityAdminHandler serves admin store-identity endpoints.
type StoreIdentityAdminHandler struct {
	svc *storeidentity.Service
}

// NewStoreIdentityAdminHandler creates a StoreIdentityAdminHandler.
func NewStoreIdentityAdminHandler(svc *storeidentity.Service) *StoreIdentityAdminHandler {
	if svc == nil {
		panic("StoreIdentityAdminHandler: nil service")
	}
	return &StoreIdentityAdminHandler{svc: svc}
}

type identityRequest struct {
	LegalName    string `json:"legal_name"`
	Phone        string `json:"phone"`
	Hours        string `json:"hours"`
	EmailGeneral string `json:"email_general"`
	EmailSales   string `json:"email_sales"`
	EmailSupport string `json:"email_support"`
}

type addressRequest struct {
	Line1    string `json:"line1"`
	Line2    string `json:"line2"`
	City     string `json:"city"`
	Region   string `json:"region"`
	Postcode string `json:"postcode"`
	Country  string `json:"country"`
	Phone    string `json:"phone"`
}

type taxIdentifierRequest struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

func (h *StoreIdentityAdminHandler) scopeStoreID(r *http.Request) (string, error) {
	id := strings.TrimSpace(ResolveStoreScopeID(r))
	if id == "" {
		return "", apperror.Validation("store scope required")
	}
	return id, nil
}

// GetIdentity handles GET /api/v1/admin/store-identity.
func (h *StoreIdentityAdminHandler) GetIdentity() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		storeID, err := h.scopeStoreID(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		id, err := h.svc.GetIdentity(r.Context(), storeID)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusOK, id)
	}
}

// PutIdentity handles PUT /api/v1/admin/store-identity.
func (h *StoreIdentityAdminHandler) PutIdentity() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		storeID, err := h.scopeStoreID(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		var req identityRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpshared.JSONError(w, apperror.Validation("invalid request body"))
			return
		}
		id, err := h.svc.SaveIdentity(r.Context(), storeID, storeidentity.IdentityInput{
			LegalName:    req.LegalName,
			Phone:        req.Phone,
			Hours:        req.Hours,
			EmailGeneral: req.EmailGeneral,
			EmailSales:   req.EmailSales,
			EmailSupport: req.EmailSupport,
		})
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusOK, id)
	}
}

// ListAddresses handles GET /api/v1/admin/store-addresses.
func (h *StoreIdentityAdminHandler) ListAddresses() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		storeID, err := h.scopeStoreID(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		addrs, err := h.svc.ListAddresses(r.Context(), storeID)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"addresses": addrs})
	}
}

// GetAddress handles GET /api/v1/admin/store-addresses/{role}.
func (h *StoreIdentityAdminHandler) GetAddress() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		storeID, err := h.scopeStoreID(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		role, err := store.ParseAddressRole(r.PathValue("role"))
		if err != nil {
			httpshared.JSONError(w, apperror.Validation(err.Error()))
			return
		}
		addr, err := h.svc.GetAddress(r.Context(), storeID, role)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusOK, addr)
	}
}

// PutAddress handles PUT /api/v1/admin/store-addresses/{role}.
func (h *StoreIdentityAdminHandler) PutAddress() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		storeID, err := h.scopeStoreID(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		role, err := store.ParseAddressRole(r.PathValue("role"))
		if err != nil {
			httpshared.JSONError(w, apperror.Validation(err.Error()))
			return
		}
		var req addressRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpshared.JSONError(w, apperror.Validation("invalid request body"))
			return
		}
		addr, err := h.svc.SaveAddress(r.Context(), storeID, role, storeidentity.AddressInput{
			Line1:    req.Line1,
			Line2:    req.Line2,
			City:     req.City,
			Region:   req.Region,
			Postcode: req.Postcode,
			Country:  req.Country,
			Phone:    req.Phone,
		})
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusOK, addr)
	}
}

// ListTaxIdentifiers handles GET /api/v1/admin/tax-identifiers.
func (h *StoreIdentityAdminHandler) ListTaxIdentifiers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		storeID, err := h.scopeStoreID(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		list, err := h.svc.ListTaxIdentifiers(r.Context(), storeID)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		if list == nil {
			list = []store.TaxIdentifier{}
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"tax_identifiers": list})
	}
}

// PutTaxIdentifier handles PUT /api/v1/admin/tax-identifiers.
func (h *StoreIdentityAdminHandler) PutTaxIdentifier() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		storeID, err := h.scopeStoreID(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		var req taxIdentifierRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpshared.JSONError(w, apperror.Validation("invalid request body"))
			return
		}
		ti, err := h.svc.UpsertTaxIdentifier(r.Context(), storeID, req.Type, req.Value)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusOK, ti)
	}
}

// DeleteTaxIdentifier handles DELETE /api/v1/admin/tax-identifiers?type=.
func (h *StoreIdentityAdminHandler) DeleteTaxIdentifier() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		storeID, err := h.scopeStoreID(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		typ := r.URL.Query().Get("type")
		if err := h.svc.DeleteTaxIdentifier(r.Context(), storeID, typ); err != nil {
			httpshared.JSONError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ListTaxIdentifierTypes handles GET /api/v1/admin/tax-identifier-types?country=.
func (h *StoreIdentityAdminHandler) ListTaxIdentifierTypes() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		types := h.svc.ListTaxIdentifierTypes(r.URL.Query().Get("country"))
		if types == nil {
			types = []store.TaxIdentifierType{}
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"types": types})
	}
}
