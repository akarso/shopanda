package http

import (
	"net/http"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// CustomerAdminHandler serves admin customer endpoints.
type CustomerAdminHandler struct {
	repo    customer.CustomerRepository
	deleter AccountDeleter
	auditor *admin.Auditor
}

// NewCustomerAdminHandler creates a CustomerAdminHandler.
func NewCustomerAdminHandler(repo customer.CustomerRepository, log logger.Logger) *CustomerAdminHandler {
	if repo == nil {
		panic("http: customer repository must not be nil")
	}
	if log == nil {
		panic("http: logger must not be nil")
	}
	return NewCustomerAdminHandlerWithDeleter(repo, nil, log)
}

// NewCustomerAdminHandlerWithDeleter creates a CustomerAdminHandler with a delete use case.
func NewCustomerAdminHandlerWithDeleter(repo customer.CustomerRepository, deleter AccountDeleter, log logger.Logger) *CustomerAdminHandler {
	if repo == nil {
		panic("http: customer repository must not be nil")
	}
	if log == nil {
		panic("http: logger must not be nil")
	}
	return NewCustomerAdminHandlerWithAuditorAndDeleter(repo, deleter, admin.NewAuditor(log))
}

// NewCustomerAdminHandlerWithAuditor creates a CustomerAdminHandler with a custom auditor.
func NewCustomerAdminHandlerWithAuditor(repo customer.CustomerRepository, auditor *admin.Auditor) *CustomerAdminHandler {
	return NewCustomerAdminHandlerWithAuditorAndDeleter(repo, nil, auditor)
}

// NewCustomerAdminHandlerWithAuditorAndDeleter creates a CustomerAdminHandler with custom dependencies.
func NewCustomerAdminHandlerWithAuditorAndDeleter(repo customer.CustomerRepository, deleter AccountDeleter, auditor *admin.Auditor) *CustomerAdminHandler {
	if repo == nil {
		panic("http: customer repository must not be nil")
	}
	if auditor == nil {
		panic("http: auditor must not be nil")
	}
	return &CustomerAdminHandler{repo: repo, deleter: deleter, auditor: auditor}
}

type customerAdminResponse struct {
	ID              string  `json:"id"`
	Email           string  `json:"email"`
	FirstName       string  `json:"first_name"`
	LastName        string  `json:"last_name"`
	Role            string  `json:"role"`
	Status          string  `json:"status"`
	EmailVerifiedAt *string `json:"email_verified_at,omitempty"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

// List handles GET /api/v1/admin/customers.
func (h *CustomerAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminID := adminIDFromRequest(r)
		details := fullAdminScopeDetailsFromRequest(r)

		offset, limit, err := parsePagination(r)
		if err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditCustomerRead,
				ResourceType: "customers",
				Result:       "error",
				Error:        err.Error(),
				Details:      details,
			})
			JSONError(w, err)
			return
		}

		details["offset"] = offset
		details["limit"] = limit

		customers, err := h.repo.ListCustomers(r.Context(), offset, limit)
		if err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditCustomerRead,
				ResourceType: "customers",
				Result:       "error",
				Error:        err.Error(),
				Details:      details,
			})
			JSONError(w, err)
			return
		}

		out := make([]customerAdminResponse, 0, len(customers))
		for i := range customers {
			out = append(out, toCustomerAdminResponse(&customers[i]))
		}

		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditCustomerRead,
			ResourceType: "customers",
			Result:       "success",
			Details:      details,
		})

		JSON(w, http.StatusOK, map[string]interface{}{
			"customers": out,
		})
	}
}

// Get handles GET /api/v1/admin/customers/{customerId}.
func (h *CustomerAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := r.PathValue("customerId")
		adminID := adminIDFromRequest(r)
		details := fullAdminScopeDetailsFromRequest(r)

		c, err := h.repo.FindByID(r.Context(), customerID)
		if err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditCustomerRead,
				ResourceType: "customer",
				ResourceID:   customerID,
				Result:       "error",
				Error:        err.Error(),
				Details:      details,
			})
			JSONError(w, err)
			return
		}
		if c == nil {
			JSONError(w, apperror.NotFound("customer not found"))
			return
		}

		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditCustomerRead,
			ResourceType: "customer",
			ResourceID:   customerID,
			Result:       "success",
			Details:      details,
		})

		JSON(w, http.StatusOK, map[string]interface{}{
			"customer": toCustomerAdminResponse(c),
		})
	}
}

// RevokeSessions handles POST /api/v1/admin/customers/{customerId}/revoke-sessions.
func (h *CustomerAdminHandler) RevokeSessions() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := r.PathValue("customerId")
		adminID := adminIDFromRequest(r)
		details := fullAdminScopeDetailsFromRequest(r)

		if err := h.repo.BumpTokenGeneration(r.Context(), customerID); err != nil {
			if apperror.Is(err, apperror.CodeNotFound) {
				JSONError(w, err)
				return
			}
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditCustomerRevoke,
				ResourceType: "customer",
				ResourceID:   customerID,
				Result:       "error",
				Error:        err.Error(),
				Details:      details,
			})
			JSONError(w, err)
			return
		}

		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditCustomerRevoke,
			ResourceType: "customer",
			ResourceID:   customerID,
			Result:       "success",
			Details:      details,
		})

		JSON(w, http.StatusOK, map[string]interface{}{
			"customer_id": customerID,
			"status":      "sessions_revoked",
		})
	}
}

// Delete handles DELETE /api/v1/admin/customers/{customerId}.
func (h *CustomerAdminHandler) Delete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := r.PathValue("customerId")
		adminID := adminIDFromRequest(r)
		details := fullAdminScopeDetailsFromRequest(r)

		if h.deleter == nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditCustomerDelete,
				ResourceType: "customer",
				ResourceID:   customerID,
				Result:       "error",
				Error:        "customer delete is not configured",
				Details:      details,
			})
			JSONError(w, apperror.Internal("customer delete is not configured"))
			return
		}

		if err := h.deleter.DeleteAccount(r.Context(), customerID); err != nil {
			if apperror.Is(err, apperror.CodeNotFound) {
				JSONError(w, err)
				return
			}
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditCustomerDelete,
				ResourceType: "customer",
				ResourceID:   customerID,
				Result:       "error",
				Error:        err.Error(),
				Details:      details,
			})
			JSONError(w, err)
			return
		}

		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditCustomerDelete,
			ResourceType: "customer",
			ResourceID:   customerID,
			Result:       "success",
			Details:      details,
		})

		JSON(w, http.StatusOK, map[string]interface{}{
			"deleted":     true,
			"customer_id": customerID,
		})
	}
}

func toCustomerAdminResponse(c *customer.Customer) customerAdminResponse {
	resp := customerAdminResponse{
		ID:        c.ID,
		Email:     c.Email,
		FirstName: c.FirstName,
		LastName:  c.LastName,
		Role:      string(c.Role),
		Status:    string(c.Status),
		CreatedAt: c.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: c.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if c.EmailVerifiedAt != nil {
		s := c.EmailVerifiedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		resp.EmailVerifiedAt = &s
	}
	return resp
}
