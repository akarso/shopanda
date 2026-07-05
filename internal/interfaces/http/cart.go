package http

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
)

// CartHandler handles cart HTTP endpoints.
type CartHandler struct {
	svc        *cartApp.Service
	extensions *extensionapp.ValueService
}

// NewCartHandler creates a new CartHandler.
func NewCartHandler(svc *cartApp.Service, extensions *extensionapp.ValueService) *CartHandler {
	return &CartHandler{svc: svc, extensions: extensions}
}

// ── request / response types ────────────────────────────────────────────

type createCartRequest struct {
	Currency string `json:"currency"`
}

type addItemRequest struct {
	VariantID  string                     `json:"variant_id"`
	Quantity   int                        `json:"quantity"`
	Extensions []cartItemExtensionInput   `json:"extensions,omitempty"`
}

type updateItemRequest struct {
	Quantity int `json:"quantity"`
}

type applyCouponRequest struct {
	Code string `json:"code"`
}

type cartResponse struct {
	ID         string             `json:"id"`
	CustomerID string             `json:"customer_id,omitempty"`
	Status     cart.CartStatus    `json:"status"`
	Currency   string             `json:"currency"`
	CouponCode string             `json:"coupon_code,omitempty"`
	Items      []cartItemResponse `json:"items"`
	CreatedAt  string             `json:"created_at"`
	UpdatedAt  string             `json:"updated_at"`
}

type cartItemResponse struct {
	VariantID  string                      `json:"variant_id"`
	Quantity   int                         `json:"quantity"`
	UnitPrice  int64                       `json:"unit_price"`
	Currency   string                      `json:"currency"`
	LineTotal  int64                       `json:"line_total"`
	Extensions []cartItemExtensionResponse `json:"extensions,omitempty"`
}

func (h *CartHandler) toCartResponse(ctx context.Context, c *cart.Cart) (cartResponse, error) {
	items := make([]cartItemResponse, len(c.Items))
	for i, item := range c.Items {
		lineTotal, _ := item.UnitPrice.MulChecked(int64(item.Quantity))
		extensions, err := cartItemExtensions(ctx, h.extensions, c.ID, item.VariantID)
		if err != nil {
			return cartResponse{}, err
		}
		items[i] = cartItemResponse{
			VariantID:  item.VariantID,
			Quantity:   item.Quantity,
			UnitPrice:  item.UnitPrice.Amount(),
			Currency:   item.UnitPrice.Currency(),
			LineTotal:  lineTotal.Amount(),
			Extensions: extensions,
		}
	}
	return cartResponse{
		ID:         c.ID,
		CustomerID: c.CustomerID,
		Status:     c.Status(),
		Currency:   c.Currency,
		CouponCode: c.CouponCode,
		Items:      items,
		CreatedAt:  c.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:  c.UpdatedAt.UTC().Format(time.RFC3339),
	}, nil
}

func (h *CartHandler) writeCart(w http.ResponseWriter, r *http.Request, c *cart.Cart, status int) {
	resp, err := h.toCartResponse(r.Context(), c)
	if err != nil {
		JSONError(w, extensionapp.MapValueError(err))
		return
	}
	JSON(w, status, map[string]interface{}{"cart": resp})
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

		userID := auth.IdentityFrom(r.Context()).UserID
		c, err := h.svc.CreateCart(r.Context(), userID, req.Currency)
		if err != nil {
			JSONError(w, err)
			return
		}

		h.writeCart(w, r, c, http.StatusCreated)
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

		c, err := h.svc.GetCart(r.Context(), cartID, auth.IdentityFrom(r.Context()).UserID)
		if err != nil {
			JSONError(w, err)
			return
		}

		h.writeCart(w, r, c, http.StatusOK)
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

		userID := auth.IdentityFrom(r.Context()).UserID
		c, err := h.svc.AddItem(r.Context(), cartID, userID, req.VariantID, req.Quantity, cartApp.AddItemOptions{
			Extensions: extensionInputsFromRequest(req.Extensions),
			UpdatedBy:  userID,
		})
		if err != nil {
			JSONError(w, extensionapp.MapValueError(err))
			return
		}

		h.writeCart(w, r, c, http.StatusOK)
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

		userID := auth.IdentityFrom(r.Context()).UserID
		c, err := h.svc.UpdateItemQuantity(r.Context(), cartID, userID, variantID, req.Quantity)
		if err != nil {
			JSONError(w, err)
			return
		}

		h.writeCart(w, r, c, http.StatusOK)
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

		userID := auth.IdentityFrom(r.Context()).UserID
		c, err := h.svc.RemoveItem(r.Context(), cartID, userID, variantID)
		if err != nil {
			JSONError(w, err)
			return
		}

		h.writeCart(w, r, c, http.StatusOK)
	}
}

// ApplyCoupon handles POST /api/v1/carts/{cartId}/coupon.
func (h *CartHandler) ApplyCoupon() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cartID := r.PathValue("cartId")
		if cartID == "" {
			JSONError(w, apperror.Validation("cart id is required"))
			return
		}

		var req applyCouponRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}
		if req.Code == "" {
			JSONError(w, apperror.Validation("code is required"))
			return
		}

		userID := auth.IdentityFrom(r.Context()).UserID
		c, err := h.svc.ApplyCoupon(r.Context(), cartID, userID, req.Code)
		if err != nil {
			JSONError(w, err)
			return
		}

		h.writeCart(w, r, c, http.StatusOK)
	}
}

// RemoveCoupon handles DELETE /api/v1/carts/{cartId}/coupon.
func (h *CartHandler) RemoveCoupon() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cartID := r.PathValue("cartId")
		if cartID == "" {
			JSONError(w, apperror.Validation("cart id is required"))
			return
		}

		userID := auth.IdentityFrom(r.Context()).UserID
		c, err := h.svc.RemoveCoupon(r.Context(), cartID, userID)
		if err != nil {
			JSONError(w, err)
			return
		}

		h.writeCart(w, r, c, http.StatusOK)
	}
}
