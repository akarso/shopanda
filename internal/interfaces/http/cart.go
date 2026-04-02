package http

import (
	"encoding/json"
	"net/http"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// CartHandler handles cart HTTP endpoints.
type CartHandler struct {
	svc *cartApp.Service
}

// NewCartHandler creates a new CartHandler.
func NewCartHandler(svc *cartApp.Service) *CartHandler {
	return &CartHandler{svc: svc}
}

// ── request / response types ────────────────────────────────────────────

type createCartRequest struct {
	Currency string `json:"currency"`
}

type addItemRequest struct {
	VariantID string `json:"variant_id"`
	Quantity  int    `json:"quantity"`
}

type updateItemRequest struct {
	Quantity int `json:"quantity"`
}

type cartResponse struct {
	ID         string             `json:"id"`
	CustomerID string             `json:"customer_id,omitempty"`
	Status     cart.CartStatus    `json:"status"`
	Currency   string             `json:"currency"`
	Items      []cartItemResponse `json:"items"`
	CreatedAt  string             `json:"created_at"`
	UpdatedAt  string             `json:"updated_at"`
}

type cartItemResponse struct {
	VariantID string `json:"variant_id"`
	Quantity  int    `json:"quantity"`
	UnitPrice int64  `json:"unit_price"`
	Currency  string `json:"currency"`
	LineTotal int64  `json:"line_total"`
}

func toCartResponse(c *cart.Cart) cartResponse {
	items := make([]cartItemResponse, len(c.Items))
	for i, item := range c.Items {
		lineTotal, _ := item.UnitPrice.MulChecked(int64(item.Quantity))
		items[i] = cartItemResponse{
			VariantID: item.VariantID,
			Quantity:  item.Quantity,
			UnitPrice: item.UnitPrice.Amount(),
			Currency:  item.UnitPrice.Currency(),
			LineTotal: lineTotal.Amount(),
		}
	}
	return cartResponse{
		ID:         c.ID,
		CustomerID: c.CustomerID,
		Status:     c.Status(),
		Currency:   c.Currency,
		Items:      items,
		CreatedAt:  c.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:  c.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

// ── handlers ────────────────────────────────────────────────────────────

// Create handles POST /api/v1/carts.
func (h *CartHandler) Create() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createCartRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}
		if !shared.IsValidCurrency(req.Currency) {
			JSONError(w, apperror.Validation("invalid currency code"))
			return
		}

		c, err := h.svc.CreateCart(r.Context(), req.Currency)
		if err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusCreated, map[string]interface{}{
			"cart": toCartResponse(c),
		})
	}
}

// Get handles GET /api/v1/carts/{cartId}.
func (h *CartHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cartID := r.PathValue("cartId")
		if cartID == "" {
			JSONError(w, apperror.Validation("cart id is required"))
			return
		}

		c, err := h.svc.GetCart(r.Context(), cartID)
		if err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"cart": toCartResponse(c),
		})
	}
}

// AddItem handles POST /api/v1/carts/{cartId}/items.
func (h *CartHandler) AddItem() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cartID := r.PathValue("cartId")
		if cartID == "" {
			JSONError(w, apperror.Validation("cart id is required"))
			return
		}

		var req addItemRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}
		if req.VariantID == "" {
			JSONError(w, apperror.Validation("variant_id is required"))
			return
		}
		if req.Quantity <= 0 {
			JSONError(w, apperror.Validation("quantity must be positive"))
			return
		}

		c, err := h.svc.AddItem(r.Context(), cartID, req.VariantID, req.Quantity)
		if err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"cart": toCartResponse(c),
		})
	}
}

// UpdateItem handles PUT /api/v1/carts/{cartId}/items/{variantId}.
func (h *CartHandler) UpdateItem() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cartID := r.PathValue("cartId")
		variantID := r.PathValue("variantId")
		if cartID == "" {
			JSONError(w, apperror.Validation("cart id is required"))
			return
		}
		if variantID == "" {
			JSONError(w, apperror.Validation("variant id is required"))
			return
		}

		var req updateItemRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}
		if req.Quantity <= 0 {
			JSONError(w, apperror.Validation("quantity must be positive"))
			return
		}

		c, err := h.svc.UpdateItemQuantity(r.Context(), cartID, variantID, req.Quantity)
		if err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"cart": toCartResponse(c),
		})
	}
}

// RemoveItem handles DELETE /api/v1/carts/{cartId}/items/{variantId}.
func (h *CartHandler) RemoveItem() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cartID := r.PathValue("cartId")
		variantID := r.PathValue("variantId")
		if cartID == "" {
			JSONError(w, apperror.Validation("cart id is required"))
			return
		}
		if variantID == "" {
			JSONError(w, apperror.Validation("variant id is required"))
			return
		}

		c, err := h.svc.RemoveItem(r.Context(), cartID, variantID)
		if err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"cart": toCartResponse(c),
		})
	}
}
