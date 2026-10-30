package storefront

import (
	"encoding/json"
	"net/http"
	"strings"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	assignmentapp "github.com/akarso/shopanda/internal/application/assignment"
	"github.com/akarso/shopanda/internal/domain/assignment"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
)

// AssignmentAccountHandler is the storefront self-service assignment API (PR-1070).
// Admin list/create/revoke is AssignmentAdminHandler (PR-1071).
type AssignmentAccountHandler struct {
	flow *assignmentapp.Flow
}

// NewAssignmentAccountHandler creates an AssignmentAccountHandler.
func NewAssignmentAccountHandler(flow *assignmentapp.Flow) *AssignmentAccountHandler {
	if flow == nil {
		panic("http: assignment flow must not be nil")
	}
	return &AssignmentAccountHandler{flow: flow}
}

type assignmentResponse struct {
	ID                string  `json:"id"`
	ParentOrderItemID string  `json:"parent_order_item_id,omitempty"`
	ChildOrderItemID  string  `json:"child_order_item_id"`
	Status            string  `json:"status"`
	CreatedAt         string  `json:"created_at"`
	RevokedAt         *string `json:"revoked_at,omitempty"`
}

type ownedParentResponse struct {
	UnitID    string `json:"unit_id"`
	OrderID   string `json:"order_id"`
	VariantID string `json:"variant_id"`
	ProductID string `json:"product_id"`
	Name      string `json:"name"`
}

type assignOwnedRequest struct {
	ChildUnitID  string `json:"child_unit_id"`
	ParentUnitID string `json:"parent_unit_id"`
}

func toAssignmentResponse(a *assignment.ProductAssignment) assignmentResponse {
	resp := assignmentResponse{
		ID:                a.ID,
		ParentOrderItemID: a.ParentOrderItemID,
		ChildOrderItemID:  a.ChildOrderItemID,
		Status:            string(a.Status()),
		CreatedAt:         a.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if a.RevokedAt != nil {
		s := a.RevokedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		resp.RevokedAt = &s
	}
	return resp
}

// ListParents handles GET /api/v1/account/assignment-parents?child_product_id=.
func (h *AssignmentAccountHandler) ListParents() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID
		childProductID := strings.TrimSpace(r.URL.Query().Get("child_product_id"))
		if childProductID == "" {
			httpshared.JSONError(w, apperror.Validation("child_product_id is required"))
			return
		}
		list, err := h.flow.ListOwnedParents(r.Context(), customerID, childProductID)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		out := make([]ownedParentResponse, 0, len(list))
		for _, p := range list {
			out = append(out, ownedParentResponse{
				UnitID:    p.UnitID,
				OrderID:   p.OrderID,
				VariantID: p.VariantID,
				ProductID: p.ProductID,
				Name:      p.Name,
			})
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"parents": out})
	}
}

// GetCurrent handles GET /api/v1/account/assignments?child_unit_id=.
func (h *AssignmentAccountHandler) GetCurrent() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID
		childUnitID := strings.TrimSpace(r.URL.Query().Get("child_unit_id"))
		if childUnitID == "" {
			httpshared.JSONError(w, apperror.Validation("child_unit_id is required"))
			return
		}
		a, err := h.flow.CurrentOwned(r.Context(), customerID, childUnitID)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		if a == nil {
			httpshared.JSON(w, http.StatusOK, map[string]interface{}{"assignment": nil})
			return
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"assignment": toAssignmentResponse(a)})
	}
}

// Assign handles POST /api/v1/account/assignments.
func (h *AssignmentAccountHandler) Assign() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID
		var req assignOwnedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpshared.JSONError(w, apperror.Validation("invalid request body"))
			return
		}
		if strings.TrimSpace(req.ChildUnitID) == "" {
			httpshared.JSONError(w, apperror.Validation("child_unit_id is required"))
			return
		}
		if strings.TrimSpace(req.ParentUnitID) == "" {
			httpshared.JSONError(w, apperror.Validation("parent_unit_id is required"))
			return
		}
		a, err := h.flow.AssignOwned(r.Context(), customerID, req.ChildUnitID, req.ParentUnitID)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"assignment": toAssignmentResponse(a)})
	}
}
