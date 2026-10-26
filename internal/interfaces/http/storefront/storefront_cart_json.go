package storefront

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/domain/cart"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type storefrontCartJSONAddRequest struct {
	VariantID  string                   `json:"variant_id"`
	Quantity   storefrontJSONQuantity   `json:"quantity"`
	Extensions []cartItemExtensionInput `json:"extensions"`
}

type storefrontCartJSONUpdateRequest struct {
	Quantity storefrontJSONQuantity `json:"quantity"`
}

type storefrontCartJSONMutationResponse struct {
	ItemCount     int    `json:"item_count"`
	TotalQuantity int    `json:"total_quantity"`
	CartURL       string `json:"cart_url"`
}

// storefrontJSONQuantity distinguishes omitted quantity (default 1) from
// explicit null / 0 / negative (all 422).
type storefrontJSONQuantity struct {
	present bool
	value   *int
}

func (q *storefrontJSONQuantity) UnmarshalJSON(b []byte) error {
	q.present = true
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		q.value = nil
		return nil
	}
	var n int
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	q.value = &n
	return nil
}

// StorefrontCSRF handles GET /storefront/csrf — JSON token for same-origin
// configurator scripts. Mints shopanda_csrf when missing.
func (h *StorefrontHandler) StorefrontCSRF() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		token := httpshared.CSRFToken(r)
		if token == "" {
			var err error
			token, err = httpshared.EnsureCSRFToken(w, r, h.trustedProxies)
			if err != nil {
				h.log.Warn("storefront.csrf.ensure_failed", map[string]interface{}{
					"path":  r.URL.Path,
					"error": err.Error(),
				})
				httpshared.JSONError(w, apperror.Internal("failed to issue csrf token"))
				return
			}
		}
		httpshared.JSON(w, http.StatusOK, map[string]string{"csrf_token": token})
	}
}

// AddToStorefrontCartJSON handles POST /storefront/cart/items.
// It mutates the SSR cart (cookie / signed-in active cart), not a headless cart_id.
// The response never includes cart_id so guests cannot pivot onto /api/v1/carts.
func (h *StorefrontHandler) AddToStorefrontCartJSON() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !storefrontJSONContentType(r) {
			httpshared.JSONError(w, apperror.Validation("Content-Type must be application/json"))
			return
		}
		var req storefrontCartJSONAddRequest
		if err := decodeStorefrontCartJSON(r, &req); err != nil {
			httpshared.JSONError(w, apperror.Validation("invalid request body"))
			return
		}
		variantID := strings.TrimSpace(req.VariantID)
		if variantID == "" {
			httpshared.JSONError(w, apperror.Validation("variant_id is required"))
			return
		}
		quantity, ok := storefrontJSONAddQuantity(req.Quantity)
		if !ok {
			httpshared.JSONError(w, apperror.Validation("quantity must be positive"))
			return
		}
		if h.carts == nil {
			httpshared.JSONError(w, apperror.Internal("storefront cart service is not configured"))
			return
		}
		currentCart, created, err := h.loadOrCreateStorefrontCart(r, w, false)
		if err != nil {
			h.logStorefrontCartError("storefront.cart.json.add.ensure_failed", err, r, map[string]interface{}{
				"variant_id": variantID,
				"quantity":   quantity,
			})
			httpshared.JSONError(w, apperror.Internal("failed to load cart"))
			return
		}
		customerID := storefrontCustomerID(r)
		updated, err := h.addLineToStorefrontCart(r, currentCart, customerID, variantID, quantity, extensionInputsFromRequest(req.Extensions))
		if err != nil {
			h.discardUnusedGuestCart(r, created, customerID, currentCart)
			h.writeStorefrontCartJSONError(w, r, err, map[string]interface{}{
				"cart_id":     currentCart.ID,
				"customer_id": customerID,
				"variant_id":  variantID,
				"quantity":    quantity,
			})
			return
		}
		if customerID == "" {
			storefrontSetCartCookie(w, r, updated.ID)
		}
		h.writeStorefrontCartJSON(w, r, updated)
	}
}

// UpdateStorefrontCartJSON handles PUT /storefront/cart/items/{variantId}.
func (h *StorefrontHandler) UpdateStorefrontCartJSON() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !storefrontJSONContentType(r) {
			httpshared.JSONError(w, apperror.Validation("Content-Type must be application/json"))
			return
		}
		variantID := strings.TrimSpace(r.PathValue("variantId"))
		if variantID == "" {
			httpshared.JSONError(w, apperror.Validation("variant_id is required"))
			return
		}
		var req storefrontCartJSONUpdateRequest
		if err := decodeStorefrontCartJSON(r, &req); err != nil {
			httpshared.JSONError(w, apperror.Validation("invalid request body"))
			return
		}
		quantity, ok := storefrontJSONRequiredPositiveQuantity(req.Quantity)
		if !ok {
			httpshared.JSONError(w, apperror.Validation("quantity must be positive"))
			return
		}
		currentCart, loaded := h.requireStorefrontCartJSON(w, r, "storefront.cart.json.update.load_failed")
		if !loaded {
			return
		}
		customerID := storefrontCustomerID(r)
		updated, err := h.carts.UpdateItemQuantity(r.Context(), currentCart.ID, customerID, variantID, quantity)
		if err != nil {
			h.writeStorefrontCartJSONError(w, r, err, map[string]interface{}{
				"cart_id":     currentCart.ID,
				"customer_id": customerID,
				"variant_id":  variantID,
				"quantity":    quantity,
			})
			return
		}
		h.writeStorefrontCartJSON(w, r, updated)
	}
}

// RemoveStorefrontCartJSON handles DELETE /storefront/cart/items/{variantId}.
func (h *StorefrontHandler) RemoveStorefrontCartJSON() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		variantID := strings.TrimSpace(r.PathValue("variantId"))
		if variantID == "" {
			httpshared.JSONError(w, apperror.Validation("variant_id is required"))
			return
		}
		currentCart, ok := h.requireStorefrontCartJSON(w, r, "storefront.cart.json.remove.load_failed")
		if !ok {
			return
		}
		customerID := storefrontCustomerID(r)
		updated, err := h.carts.RemoveItem(r.Context(), currentCart.ID, customerID, variantID)
		if err != nil {
			h.writeStorefrontCartJSONError(w, r, err, map[string]interface{}{
				"cart_id":     currentCart.ID,
				"customer_id": customerID,
				"variant_id":  variantID,
			})
			return
		}
		h.writeStorefrontCartJSON(w, r, updated)
	}
}

const storefrontGuestCartDiscardTimeout = 5 * time.Second

func storefrontJSONAddQuantity(q storefrontJSONQuantity) (int, bool) {
	if !q.present {
		return 1, true
	}
	return storefrontJSONRequiredPositiveQuantity(q)
}

func storefrontJSONRequiredPositiveQuantity(q storefrontJSONQuantity) (int, bool) {
	if !q.present || q.value == nil || *q.value <= 0 {
		return 0, false
	}
	return *q.value, true
}

func (h *StorefrontHandler) discardUnusedGuestCart(r *http.Request, created bool, customerID string, currentCart *cart.Cart) {
	if !created || customerID != "" || currentCart == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), storefrontGuestCartDiscardTimeout)
	defer cancel()
	if err := h.carts.DiscardEmptyGuestCart(ctx, currentCart.ID); err != nil {
		h.logStorefrontCartError("storefront.cart.json.add.discard_failed", err, r, map[string]interface{}{
			"cart_id": currentCart.ID,
		})
	}
}

func decodeStorefrontCartJSON(r *http.Request, dest interface{}) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return err
	}
	return requireSingleJSONValue(dec)
}

func requireSingleJSONValue(dec *json.Decoder) error {
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		return errors.New("request body must contain exactly one JSON value")
	}
	return nil
}

func storefrontJSONContentType(r *http.Request) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && media == "application/json"
}

func (h *StorefrontHandler) requireStorefrontCartJSON(w http.ResponseWriter, r *http.Request, loadEvent string) (*cart.Cart, bool) {
	if h.carts == nil {
		httpshared.JSONError(w, apperror.Internal("storefront cart service is not configured"))
		return nil, false
	}
	currentCart, err := h.currentCart(r)
	if err != nil {
		h.logStorefrontCartError(loadEvent, err, r, nil)
		httpshared.JSONError(w, apperror.Internal("failed to load cart"))
		return nil, false
	}
	if currentCart == nil {
		httpshared.JSONError(w, apperror.NotFound("cart not found"))
		return nil, false
	}
	return currentCart, true
}

func (h *StorefrontHandler) writeStorefrontCartJSONError(w http.ResponseWriter, r *http.Request, err error, extra map[string]interface{}) {
	if vf, ok := cartApp.ValidationFailedFrom(err); ok {
		data := map[string]interface{}{}
		if len(vf.Issues) > 0 {
			data["validation_errors"] = vf.Issues
		}
		httpshared.JSONWithError(w, http.StatusUnprocessableEntity, data, apperror.CartValidationFailed(vf.Error()))
		return
	}
	mapped := extensionapp.MapValueError(err)
	if storefrontCartJSONClientError(mapped) {
		httpshared.JSONError(w, mapped)
		return
	}
	h.logStorefrontCartError(storefrontCartJSONFailureEvent(r), err, r, extra)
	httpshared.JSONError(w, apperror.Internal("failed to update cart"))
}

func storefrontCartJSONClientError(err error) bool {
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		return false
	}
	switch appErr.Code {
	case apperror.CodeValidation, apperror.CodeNotFound, apperror.CodeForbidden,
		apperror.CodeConflict, apperror.CodeUnknownFieldCode, apperror.CodeFieldValidationFailed,
		apperror.CodeCartValidationFailed, apperror.CodeForbiddenPrivateField:
		return true
	default:
		return false
	}
}

func storefrontCartJSONFailureEvent(r *http.Request) string {
	switch r.Method {
	case http.MethodPost:
		return "storefront.cart.json.add.failed"
	case http.MethodPut:
		return "storefront.cart.json.update.failed"
	case http.MethodDelete:
		return "storefront.cart.json.remove.failed"
	default:
		return "storefront.cart.json.failed"
	}
}

func (h *StorefrontHandler) writeStorefrontCartJSON(w http.ResponseWriter, r *http.Request, updated *cart.Cart) {
	if storefrontIsHTMX(r) {
		w.Header().Set("HX-Trigger", "cart-updated")
	}
	httpshared.JSON(w, http.StatusOK, storefrontCartJSONMutationResponse{
		ItemCount:     len(updated.Items),
		TotalQuantity: updated.TotalQuantity(),
		CartURL:       "/cart",
	})
}
