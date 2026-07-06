package http

import (
	"encoding/json"
	"net/http"
	"time"

	checkoutApp "github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
)

// CheckoutHandler handles checkout HTTP endpoints.
type CheckoutHandler struct {
	svc *checkoutApp.Service
}

// NewCheckoutHandler creates a CheckoutHandler.
func NewCheckoutHandler(svc *checkoutApp.Service) *CheckoutHandler {
	if svc == nil {
		panic("http: checkout service must not be nil")
	}
	return &CheckoutHandler{svc: svc}
}

// ── request / response types ────────────────────────────────────────────

type checkoutRequest struct {
	CartID            string                 `json:"cart_id"`
	ContactEmail      string                 `json:"contact_email"`
	Address           checkoutAddressRequest `json:"address"`
	StoreCreditAmount int64                  `json:"store_credit_amount"`
}

type checkoutAddressRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Street    string `json:"street"`
	City      string `json:"city"`
	Postcode  string `json:"postcode"`
	Country   string `json:"country"`
}

type checkoutOrderResponse struct {
	ID                 string            `json:"id"`
	CustomerID         string            `json:"customer_id"`
	Status             order.OrderStatus `json:"status"`
	Currency           string            `json:"currency"`
	Items              []orderItemResp   `json:"items"`
	TotalAmount        int64             `json:"total_amount"`
	StoreCreditApplied int64             `json:"store_credit_applied"`
	PayableAmount      int64             `json:"payable_amount"`
	CreatedAt          string            `json:"created_at"`
}

type orderItemResp struct {
	VariantID  string                      `json:"variant_id"`
	SKU        string                      `json:"sku"`
	Name       string                      `json:"name"`
	Quantity   int                         `json:"quantity"`
	UnitPrice  int64                       `json:"unit_price"`
	Currency   string                      `json:"currency"`
	Extensions []cartItemExtensionResponse `json:"extensions,omitempty"`
}

// ── handler ─────────────────────────────────────────────────────────────

// StartCheckout handles POST /api/v1/checkout.
func (h *CheckoutHandler) StartCheckout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req checkoutRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		if req.CartID == "" {
			JSONError(w, apperror.Validation("cart_id is required"))
			return
		}

		customerID := auth.IdentityFrom(r.Context()).UserID

		// Guest contact_email rules are enforced in checkout.Service.StartCheckout.
		cctx, err := h.svc.StartCheckout(r.Context(), req.CartID, customerID, checkoutApp.Input{
			ContactEmail:      req.ContactEmail,
			StoreCreditAmount: req.StoreCreditAmount,
			Address: checkoutApp.Address{
				FirstName: req.Address.FirstName,
				LastName:  req.Address.LastName,
				Street:    req.Address.Street,
				City:      req.Address.City,
				Postcode:  req.Address.Postcode,
				Country:   req.Address.Country,
			},
		})
		if err != nil {
			JSONError(w, err)
			return
		}

		items := make([]orderItemResp, 0, len(cctx.Order.Items()))
		for _, item := range cctx.Order.Items() {
			items = append(items, orderItemResp{
				VariantID: item.VariantID,
				SKU:       item.SKU,
				Name:      item.Name,
				Quantity:  item.Quantity,
				UnitPrice: item.UnitPrice.Amount(),
				Currency:  item.UnitPrice.Currency(),
			})
		}

		payable, err := cctx.Order.PayableAmount()
		if err != nil {
			JSONError(w, apperror.Wrap(apperror.CodeInternal, "payable amount failed", err))
			return
		}

		resp := map[string]interface{}{
			"order": checkoutOrderResponse{
				ID:                 cctx.Order.ID,
				CustomerID:         cctx.Order.CustomerID,
				Status:             cctx.Order.Status(),
				Currency:           cctx.Order.Currency,
				Items:              items,
				TotalAmount:        cctx.Order.TotalAmount.Amount(),
				StoreCreditApplied: cctx.Order.StoreCreditApplied.Amount(),
				PayableAmount:      payable.Amount(),
				CreatedAt:          cctx.Order.CreatedAt.UTC().Format(time.RFC3339),
			},
		}

		// Include payment metadata for async providers (e.g. Stripe).
		if cs, ok := cctx.GetMeta("client_secret"); ok {
			resp["payment"] = map[string]interface{}{
				"client_secret": cs,
			}
		}

		JSON(w, http.StatusCreated, resp)
	}
}
