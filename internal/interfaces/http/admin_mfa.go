package http

import (
	"encoding/json"
	"net/http"

	mfaApp "github.com/akarso/shopanda/internal/application/mfa"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
)

// AdminMFAHandler serves authenticated admin MFA enrollment endpoints.
type AdminMFAHandler struct {
	mfa *mfaApp.Service
}

// NewAdminMFAHandler creates an AdminMFAHandler.
func NewAdminMFAHandler(mfa *mfaApp.Service) *AdminMFAHandler {
	if mfa == nil {
		panic("http: mfa service must not be nil")
	}
	return &AdminMFAHandler{mfa: mfa}
}

// Status handles GET /api/v1/admin/mfa.
func (h *AdminMFAHandler) Status() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID
		status, err := h.mfa.GetStatus(r.Context(), customerID)
		if err != nil {
			JSONError(w, err)
			return
		}
		JSON(w, http.StatusOK, map[string]interface{}{
			"mfa": status,
		})
	}
}

// EnrollBegin handles POST /api/v1/admin/mfa/enroll.
func (h *AdminMFAHandler) EnrollBegin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID
		out, err := h.mfa.BeginEnrollment(r.Context(), customerID)
		if err != nil {
			JSONError(w, err)
			return
		}
		JSON(w, http.StatusOK, map[string]interface{}{
			"enrollment": out,
		})
	}
}

type confirmMFAEnrollmentBody struct {
	Code string `json:"code"`
}

// EnrollConfirm handles POST /api/v1/admin/mfa/enroll/confirm.
func (h *AdminMFAHandler) EnrollConfirm() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID
		var req confirmMFAEnrollmentBody
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}
		out, err := h.mfa.ConfirmEnrollment(r.Context(), customerID, req.Code)
		if err != nil {
			JSONError(w, err)
			return
		}
		JSON(w, http.StatusOK, map[string]interface{}{
			"recovery_codes": out.RecoveryCodes,
		})
	}
}

type mfaPasswordBody struct {
	Password string `json:"password"`
}

// Disable handles DELETE /api/v1/admin/mfa.
func (h *AdminMFAHandler) Disable() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID
		var req mfaPasswordBody
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}
		if err := h.mfa.Disable(r.Context(), customerID, req.Password); err != nil {
			JSONError(w, err)
			return
		}
		JSON(w, http.StatusOK, map[string]interface{}{
			"status": "disabled",
		})
	}
}

// RegenerateRecoveryCodes handles POST /api/v1/admin/mfa/recovery/regenerate.
func (h *AdminMFAHandler) RegenerateRecoveryCodes() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID
		var req mfaPasswordBody
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}
		out, err := h.mfa.RegenerateRecoveryCodes(r.Context(), customerID, req.Password)
		if err != nil {
			JSONError(w, err)
			return
		}
		JSON(w, http.StatusOK, map[string]interface{}{
			"recovery_codes": out.RecoveryCodes,
		})
	}
}
