package http

import (
	"encoding/json"
	"net/http"

	returnsApp "github.com/akarso/shopanda/internal/application/returns"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
)

// ReturnAccountHandler serves customer return endpoints.
type ReturnAccountHandler struct {
	returns *returnsApp.Service
}

// NewReturnAccountHandler creates a ReturnAccountHandler.
func NewReturnAccountHandler(returns *returnsApp.Service) *ReturnAccountHandler {
	if returns == nil {
		panic("http: return service must not be nil")
	}
	return &ReturnAccountHandler{returns: returns}
}

// List handles GET /api/v1/account/returns.
func (h *ReturnAccountHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID
		list, err := h.returns.ListByCustomerID(r.Context(), customerID)
		if err != nil {
			JSONError(w, err)
			return
		}
		JSON(w, http.StatusOK, map[string]interface{}{
			"returns": toReturnResponses(list),
		})
	}
}

// Get handles GET /api/v1/account/returns/{returnId}.
func (h *ReturnAccountHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID
		returnID := r.PathValue("returnId")

		ret, err := h.returns.Get(r.Context(), returnID)
		if err != nil {
			JSONError(w, err)
			return
		}
		if ret.CustomerID != "" && ret.CustomerID != customerID {
			JSONError(w, apperror.Forbidden("return not found"))
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"return": toReturnResponse(ret),
		})
	}
}

type requestReturnLine struct {
	VariantID string `json:"variant_id"`
	Quantity  int    `json:"quantity"`
}

type requestReturnBody struct {
	Reason string              `json:"reason"`
	Lines  []requestReturnLine `json:"lines"`
}

// Request handles POST /api/v1/orders/{orderId}/returns.
func (h *ReturnAccountHandler) Request() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID
		orderID := r.PathValue("orderId")

		var req requestReturnBody
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		lines := make([]returnsApp.RequestLine, 0, len(req.Lines))
		for _, line := range req.Lines {
			lines = append(lines, returnsApp.RequestLine{
				VariantID: line.VariantID,
				Quantity:  line.Quantity,
			})
		}

		ret, err := h.returns.RequestReturn(r.Context(), orderID, customerID, req.Reason, lines)
		if err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusCreated, map[string]interface{}{
			"return": toReturnResponse(ret),
		})
	}
}

// Cancel handles POST /api/v1/account/returns/{returnId}/cancel.
func (h *ReturnAccountHandler) Cancel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID
		returnID := r.PathValue("returnId")

		ret, err := h.returns.Get(r.Context(), returnID)
		if err != nil {
			JSONError(w, err)
			return
		}
		if ret.CustomerID != "" && ret.CustomerID != customerID {
			JSONError(w, apperror.Forbidden("return not found"))
			return
		}

		ret, err = h.returns.Cancel(r.Context(), returnID)
		if err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"return": toReturnResponse(ret),
		})
	}
}

// ListByOrder handles GET /api/v1/orders/{orderId}/returns.
func (h *ReturnAccountHandler) ListByOrder() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID
		orderID := r.PathValue("orderId")

		list, err := h.returns.ListByOrderForCustomer(r.Context(), orderID, customerID)
		if err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"returns": toReturnResponses(list),
		})
	}
}

// ReturnableLines handles GET /api/v1/orders/{orderId}/returnable-lines.
func (h *ReturnAccountHandler) ReturnableLines() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID
		orderID := r.PathValue("orderId")

		lines, err := h.returns.ReturnableLines(r.Context(), orderID, customerID)
		if err != nil {
			JSONError(w, err)
			return
		}

		out := make([]map[string]interface{}, 0, len(lines))
		for _, line := range lines {
			out = append(out, map[string]interface{}{
				"variant_id": line.VariantID,
				"sku":        line.SKU,
				"name":       line.Name,
				"ordered":    line.Ordered,
				"returnable": line.Returnable,
				"unit_price": line.UnitPrice.Amount(),
				"currency":   line.UnitPrice.Currency(),
			})
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"lines": out,
		})
	}
}
