package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	storeidentity "github.com/akarso/shopanda/internal/application/storeidentity"
	"github.com/akarso/shopanda/internal/domain/store"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// StoreIdentityAdminHandler serves admin store-identity endpoints.
type StoreIdentityAdminHandler struct {
	svc     *storeidentity.Service
	auditor *adminapp.Auditor
}

// NewStoreIdentityAdminHandler creates a StoreIdentityAdminHandler with a default auditor.
func NewStoreIdentityAdminHandler(svc *storeidentity.Service) *StoreIdentityAdminHandler {
	return NewStoreIdentityAdminHandlerWithAuditor(svc, adminapp.NewAuditor(logger.New("info")))
}

// NewStoreIdentityAdminHandlerWithAuditor creates a StoreIdentityAdminHandler with a custom auditor.
func NewStoreIdentityAdminHandlerWithAuditor(svc *storeidentity.Service, auditor *adminapp.Auditor) *StoreIdentityAdminHandler {
	if svc == nil {
		panic("StoreIdentityAdminHandler: nil service")
	}
	if auditor == nil {
		panic("StoreIdentityAdminHandler: nil auditor")
	}
	return &StoreIdentityAdminHandler{svc: svc, auditor: auditor}
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

func (h *StoreIdentityAdminHandler) audit(r *http.Request, resourceType, resourceID string, details map[string]interface{}, err error) {
	merged := mergeAuditDetails(details, fullAdminScopeDetailsFromRequest(r))
	result := "success"
	errMsg := ""
	if err != nil {
		result = "error"
		errMsg = err.Error()
	}
	h.auditor.LogAction(r.Context(), adminapp.AuditEntry{
		AdminID:      adminIDFromRequest(r),
		Action:       adminapp.AuditSettingsChange,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
}

func decodeStrict(r *http.Request, dst interface{}) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return apperror.Validation("invalid request body")
	}
	if dec.More() {
		return apperror.Validation("invalid request body")
	}
	return nil
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

// PutIdentity handles PUT /api/v1/admin/store-identity (partial update).
func (h *StoreIdentityAdminHandler) PutIdentity() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		storeID, err := h.scopeStoreID(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		var patch storeidentity.IdentityPatch
		if err := decodeStrict(r, &patch); err != nil {
			h.audit(r, "store_identity", storeID, map[string]interface{}{"store_id": storeID}, err)
			httpshared.JSONError(w, err)
			return
		}
		id, err := h.svc.PatchIdentity(r.Context(), storeID, patch)
		h.audit(r, "store_identity", storeID, map[string]interface{}{
			"store_id": storeID,
			"fields":   patch.ChangedFields(),
		}, err)
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

// PutAddress handles PUT /api/v1/admin/store-addresses/{role} (partial update).
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
		var patch storeidentity.AddressPatch
		if err := decodeStrict(r, &patch); err != nil {
			h.audit(r, "store_address", storeID, map[string]interface{}{"role": string(role)}, err)
			httpshared.JSONError(w, err)
			return
		}
		addr, err := h.svc.PatchAddress(r.Context(), storeID, role, patch)
		h.audit(r, "store_address", storeID, map[string]interface{}{
			"store_id": storeID,
			"role":     string(role),
			"fields":   patch.ChangedFields(),
		}, err)
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
		if err := decodeStrict(r, &req); err != nil {
			h.audit(r, "store_tax_identifier", storeID, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		ti, err := h.svc.UpsertTaxIdentifier(r.Context(), storeID, req.Type, req.Value)
		typ := strings.ToLower(strings.TrimSpace(req.Type))
		if utf8.RuneCountInString(typ) > store.MaxTaxTypeLen {
			typ = string([]rune(typ)[:store.MaxTaxTypeLen])
		}
		details := map[string]interface{}{"store_id": storeID, "type": typ, "fields": []string{"type", "value"}}
		h.audit(r, "store_tax_identifier", storeID, details, err)
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
		typ := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("type")))
		auditType := typ
		if utf8.RuneCountInString(auditType) > store.MaxTaxTypeLen {
			auditType = string([]rune(auditType)[:store.MaxTaxTypeLen])
		}
		err = h.svc.DeleteTaxIdentifier(r.Context(), storeID, typ)
		h.audit(r, "store_tax_identifier", storeID, map[string]interface{}{
			"store_id": storeID,
			"type":     auditType,
			"fields":   []string{"type"},
			"op":       "delete",
		}, err)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ListTaxIdentifierTypes handles GET /api/v1/admin/tax-identifier-types?country=.
func (h *StoreIdentityAdminHandler) ListTaxIdentifierTypes() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		types, err := h.svc.ListTaxIdentifierTypes(r.URL.Query().Get("country"))
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		if types == nil {
			types = []store.TaxIdentifierType{}
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"types": types})
	}
}
