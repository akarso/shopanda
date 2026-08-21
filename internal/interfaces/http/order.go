package http

import (
	"context"
	"net/http"
	"time"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
)

// OrderHandler handles order read endpoints.
type OrderHandler struct {
	orders     order.OrderRepository
	extensions *extensionapp.ValueService
}

// NewOrderHandler creates an OrderHandler.
func NewOrderHandler(orders order.OrderRepository, extensions *extensionapp.ValueService) *OrderHandler {
	if orders == nil {
		panic("http: order repository must not be nil")
	}
	return &OrderHandler{orders: orders, extensions: extensions}
}

// Get handles GET /api/v1/orders/{orderId}.
func (h *OrderHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderID := r.PathValue("orderId")
		if orderID == "" {
			JSONError(w, apperror.Validation("order id is required"))
			return
		}

		customerID := auth.IdentityFrom(r.Context()).UserID

		o, err := h.orders.FindByID(r.Context(), orderID)
		if err != nil {
			JSONError(w, err)
			return
		}
		if o == nil {
			JSONError(w, apperror.NotFound("order not found"))
			return
		}
		if o.CustomerID != customerID {
			JSONError(w, apperror.Forbidden("order not found"))
			return
		}

		resp, err := ToOrderResponse(r.Context(), h.extensions, o)
		if err != nil {
			JSONError(w, extensionapp.MapValueError(err))
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"order": resp,
		})
	}
}

// List handles GET /api/v1/orders.
func (h *OrderHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID

		orders, err := h.orders.FindByCustomerID(r.Context(), customerID)
		if err != nil {
			JSONError(w, err)
			return
		}

		out := make([]OrderResponse, 0, len(orders))
		for i := range orders {
			resp, err := ToOrderResponse(r.Context(), h.extensions, &orders[i])
			if err != nil {
				JSONError(w, extensionapp.MapValueError(err))
				return
			}
			out = append(out, resp)
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"orders": out,
		})
	}
}

// ── response types ──────────────────────────────────────────────────────

type OrderResponse struct {
	ID          string            `json:"id"`
	CustomerID  string            `json:"customer_id"`
	Status      order.OrderStatus `json:"status"`
	Currency    string            `json:"currency"`
	Items       []orderItemResp   `json:"items"`
	TotalAmount int64             `json:"total_amount"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
}

func ToOrderResponse(ctx context.Context, extensions *extensionapp.ValueService, o *order.Order) (OrderResponse, error) {
	orderItems := o.Items()
	variantIDs := make([]string, len(orderItems))
	for i, item := range orderItems {
		variantIDs[i] = item.VariantID
	}
	extByVariant, err := orderItemExtensionsMap(ctx, extensions, o.ID, variantIDs)
	if err != nil {
		return OrderResponse{}, err
	}

	items := make([]orderItemResp, 0, len(orderItems))
	for _, item := range orderItems {
		items = append(items, orderItemResp{
			VariantID:  item.VariantID,
			SKU:        item.SKU,
			Name:       item.Name,
			Quantity:   item.Quantity,
			UnitPrice:  item.UnitPrice.Amount(),
			Currency:   item.UnitPrice.Currency(),
			Extensions: orderItemExtensionsFromMap(extByVariant, item.VariantID),
		})
	}
	return OrderResponse{
		ID:          o.ID,
		CustomerID:  o.CustomerID,
		Status:      o.Status(),
		Currency:    o.Currency,
		Items:       items,
		TotalAmount: o.TotalAmount.Amount(),
		CreatedAt:   o.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   o.UpdatedAt.UTC().Format(time.RFC3339),
	}, nil
}
