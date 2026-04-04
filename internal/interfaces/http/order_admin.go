package http

import (
	"net/http"

	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// OrderAdminHandler serves admin order endpoints.
type OrderAdminHandler struct {
	orders order.OrderRepository
}

// NewOrderAdminHandler creates an OrderAdminHandler.
func NewOrderAdminHandler(orders order.OrderRepository) *OrderAdminHandler {
	if orders == nil {
		panic("http: order repository must not be nil")
	}
	return &OrderAdminHandler{orders: orders}
}

// List handles GET /api/v1/admin/orders.
func (h *OrderAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := parsePagination(r)
		if err != nil {
			JSONError(w, err)
			return
		}

		orders, err := h.orders.List(r.Context(), offset, limit)
		if err != nil {
			JSONError(w, err)
			return
		}

		out := make([]orderResponse, 0, len(orders))
		for i := range orders {
			out = append(out, toOrderResponse(&orders[i]))
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"orders": out,
		})
	}
}

// Get handles GET /api/v1/admin/orders/{orderId}.
func (h *OrderAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderID := r.PathValue("orderId")
		if orderID == "" {
			JSONError(w, apperror.Validation("order id is required"))
			return
		}

		o, err := h.orders.FindByID(r.Context(), orderID)
		if err != nil {
			JSONError(w, err)
			return
		}
		if o == nil {
			JSONError(w, apperror.NotFound("order not found"))
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"order": toOrderResponse(o),
		})
	}
}
