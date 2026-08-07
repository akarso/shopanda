package groups

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/customergroup"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

// AdminHandler serves B2B customer group admin endpoints.
type AdminHandler struct {
	groups    customergroup.Repository
	customers customer.CustomerRepository
}

// NewAdminHandler creates an AdminHandler.
func NewAdminHandler(groups customergroup.Repository, customers customer.CustomerRepository) *AdminHandler {
	if groups == nil {
		panic("b2b groups admin: groups repository must not be nil")
	}
	if customers == nil {
		panic("b2b groups admin: customers repository must not be nil")
	}
	return &AdminHandler{groups: groups, customers: customers}
}

type groupWriteRequest struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type groupUpdateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type assignGroupRequest struct {
	GroupID string `json:"group_id"`
}

type groupResponse struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func toGroupResponse(g customergroup.Group) groupResponse {
	return groupResponse{
		ID:          g.ID,
		Code:        g.Code,
		Name:        g.Name,
		Description: g.Description,
		CreatedAt:   g.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   g.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// List handles GET /api/v1/admin/customer-groups.
func (h *AdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := shophttp.ParsePagination(r)
		if err != nil {
			shophttp.JSONError(w, apperror.Validation(err.Error()))
			return
		}
		groups, err := h.groups.List(r.Context(), offset, limit)
		if err != nil {
			shophttp.JSONError(w, apperror.Wrap(apperror.CodeInternal, "list customer groups failed", err))
			return
		}
		resp := make([]groupResponse, len(groups))
		for i := range groups {
			resp[i] = toGroupResponse(groups[i])
		}
		shophttp.JSON(w, http.StatusOK, map[string]interface{}{"groups": resp})
	}
}

// Get handles GET /api/v1/admin/customer-groups/{groupId}.
func (h *AdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupID := strings.TrimSpace(r.PathValue("groupId"))
		g, err := h.groups.FindByID(r.Context(), groupID)
		if err != nil {
			shophttp.JSONError(w, apperror.Wrap(apperror.CodeInternal, "get customer group failed", err))
			return
		}
		if g == nil {
			shophttp.JSONError(w, apperror.NotFound("customer group not found"))
			return
		}
		shophttp.JSON(w, http.StatusOK, map[string]interface{}{"group": toGroupResponse(*g)})
	}
}

// Create handles POST /api/v1/admin/customer-groups.
func (h *AdminHandler) Create() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req groupWriteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			shophttp.JSONError(w, apperror.Validation("invalid JSON body"))
			return
		}
		g, err := customergroup.NewGroup(id.New(), req.Code, req.Name, req.Description)
		if err != nil {
			shophttp.JSONError(w, apperror.Validation(err.Error()))
			return
		}
		if err := h.groups.Save(r.Context(), &g); err != nil {
			if strings.Contains(err.Error(), "code already exists") {
				shophttp.JSONError(w, apperror.Validation("customer group code already exists"))
				return
			}
			shophttp.JSONError(w, apperror.Wrap(apperror.CodeInternal, "create customer group failed", err))
			return
		}
		shophttp.JSON(w, http.StatusCreated, map[string]interface{}{"group": toGroupResponse(g)})
	}
}

// Update handles PUT /api/v1/admin/customer-groups/{groupId}.
func (h *AdminHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupID := strings.TrimSpace(r.PathValue("groupId"))
		g, err := h.groups.FindByID(r.Context(), groupID)
		if err != nil {
			shophttp.JSONError(w, apperror.Wrap(apperror.CodeInternal, "update customer group failed", err))
			return
		}
		if g == nil {
			shophttp.JSONError(w, apperror.NotFound("customer group not found"))
			return
		}

		var req groupUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			shophttp.JSONError(w, apperror.Validation("invalid JSON body"))
			return
		}
		if err := g.Update(req.Name, req.Description); err != nil {
			shophttp.JSONError(w, apperror.Validation(err.Error()))
			return
		}
		if err := h.groups.Save(r.Context(), g); err != nil {
			shophttp.JSONError(w, apperror.Wrap(apperror.CodeInternal, "update customer group failed", err))
			return
		}
		shophttp.JSON(w, http.StatusOK, map[string]interface{}{"group": toGroupResponse(*g)})
	}
}

// Delete handles DELETE /api/v1/admin/customer-groups/{groupId}.
func (h *AdminHandler) Delete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupID := strings.TrimSpace(r.PathValue("groupId"))
		g, err := h.groups.FindByID(r.Context(), groupID)
		if err != nil {
			shophttp.JSONError(w, apperror.Wrap(apperror.CodeInternal, "delete customer group failed", err))
			return
		}
		if g == nil {
			shophttp.JSONError(w, apperror.NotFound("customer group not found"))
			return
		}
		if err := h.groups.Delete(r.Context(), groupID); err != nil {
			if strings.Contains(err.Error(), "not found") {
				shophttp.JSONError(w, apperror.NotFound("customer group not found"))
				return
			}
			shophttp.JSONError(w, apperror.Wrap(apperror.CodeInternal, "delete customer group failed", err))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// AssignCustomer handles PUT /api/v1/admin/customers/{customerId}/customer-group.
func (h *AdminHandler) AssignCustomer() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := strings.TrimSpace(r.PathValue("customerId"))
		cust, err := h.customers.FindByID(r.Context(), customerID)
		if err != nil {
			shophttp.JSONError(w, apperror.Wrap(apperror.CodeInternal, "assign customer group failed", err))
			return
		}
		if cust == nil {
			shophttp.JSONError(w, apperror.NotFound("customer not found"))
			return
		}

		var req assignGroupRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			shophttp.JSONError(w, apperror.Validation("invalid JSON body"))
			return
		}
		groupID := strings.TrimSpace(req.GroupID)
		if groupID == "" {
			shophttp.JSONError(w, apperror.Validation("group_id is required"))
			return
		}
		g, err := h.groups.FindByID(r.Context(), groupID)
		if err != nil {
			shophttp.JSONError(w, apperror.Wrap(apperror.CodeInternal, "assign customer group failed", err))
			return
		}
		if g == nil {
			shophttp.JSONError(w, apperror.NotFound("customer group not found"))
			return
		}
		if err := h.groups.AssignCustomer(r.Context(), customerID, groupID); err != nil {
			if strings.Contains(err.Error(), "not found") {
				shophttp.JSONError(w, apperror.NotFound("customer or group not found"))
				return
			}
			shophttp.JSONError(w, apperror.Wrap(apperror.CodeInternal, "assign customer group failed", err))
			return
		}
		shophttp.JSON(w, http.StatusOK, map[string]interface{}{"group": toGroupResponse(*g)})
	}
}

// RemoveCustomer handles DELETE /api/v1/admin/customers/{customerId}/customer-group.
func (h *AdminHandler) RemoveCustomer() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := strings.TrimSpace(r.PathValue("customerId"))
		if err := h.groups.RemoveCustomer(r.Context(), customerID); err != nil {
			shophttp.JSONError(w, apperror.Wrap(apperror.CodeInternal, "remove customer group failed", err))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// GetCustomerGroup handles GET /api/v1/admin/customers/{customerId}/customer-group.
func (h *AdminHandler) GetCustomerGroup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := strings.TrimSpace(r.PathValue("customerId"))
		cust, err := h.customers.FindByID(r.Context(), customerID)
		if err != nil {
			shophttp.JSONError(w, apperror.Wrap(apperror.CodeInternal, "get customer group failed", err))
			return
		}
		if cust == nil {
			shophttp.JSONError(w, apperror.NotFound("customer not found"))
			return
		}
		g, err := h.groups.FindGroupByCustomerID(r.Context(), customerID)
		if err != nil {
			shophttp.JSONError(w, apperror.Wrap(apperror.CodeInternal, "get customer group failed", err))
			return
		}
		if g == nil {
			shophttp.JSON(w, http.StatusOK, map[string]interface{}{"group": nil})
			return
		}
		shophttp.JSON(w, http.StatusOK, map[string]interface{}{"group": toGroupResponse(*g)})
	}
}
