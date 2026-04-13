package http

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/legal"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
)

// AccountDeleter abstracts the account deletion use case.
type AccountDeleter interface {
	DeleteAccount(ctx context.Context, customerID string) error
}

// AccountHandler serves customer account endpoints (consent + GDPR).
type AccountHandler struct {
	customers customer.CustomerRepository
	orders    order.OrderRepository
	consents  legal.ConsentRepository
	deleter   AccountDeleter
}

// NewAccountHandler creates an AccountHandler.
func NewAccountHandler(
	customers customer.CustomerRepository,
	orders order.OrderRepository,
	consents legal.ConsentRepository,
	deleter AccountDeleter,
) *AccountHandler {
	if customers == nil {
		panic("AccountHandler: customers repository must not be nil")
	}
	if orders == nil {
		panic("AccountHandler: orders repository must not be nil")
	}
	if consents == nil {
		panic("AccountHandler: consents repository must not be nil")
	}
	if deleter == nil {
		panic("AccountHandler: deleter must not be nil")
	}
	return &AccountHandler{
		customers: customers,
		orders:    orders,
		consents:  consents,
		deleter:   deleter,
	}
}

// consentResponse is the JSON shape for consent preferences.
type consentResponse struct {
	Necessary bool   `json:"necessary"`
	Analytics bool   `json:"analytics"`
	Marketing bool   `json:"marketing"`
	UpdatedAt string `json:"updated_at"`
}

// GetConsent handles GET /api/v1/account/consent.
func (h *AccountHandler) GetConsent() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID

		c, err := h.consents.FindByCustomerID(r.Context(), customerID)
		if err != nil {
			JSONError(w, err)
			return
		}
		if c == nil {
			// No consent record yet — return defaults.
			JSON(w, http.StatusOK, map[string]interface{}{
				"consent": consentResponse{
					Necessary: true,
					Analytics: false,
					Marketing: false,
				},
			})
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"consent": consentResponse{
				Necessary: c.Necessary,
				Analytics: c.Analytics,
				Marketing: c.Marketing,
				UpdatedAt: c.UpdatedAt.UTC().Format(time.RFC3339),
			},
		})
	}
}

// consentRequest is the input for updating consent.
type consentRequest struct {
	Analytics bool `json:"analytics"`
	Marketing bool `json:"marketing"`
}

// UpdateConsent handles PUT /api/v1/account/consent.
func (h *AccountHandler) UpdateConsent() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID

		var req consentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		consent, err := legal.NewConsent(customerID)
		if err != nil {
			JSONError(w, apperror.Wrap(apperror.CodeInternal, "consent creation failed", err))
			return
		}
		consent.Update(req.Analytics, req.Marketing)

		if err := h.consents.Upsert(r.Context(), &consent); err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"consent": consentResponse{
				Necessary: consent.Necessary,
				Analytics: consent.Analytics,
				Marketing: consent.Marketing,
				UpdatedAt: consent.UpdatedAt.UTC().Format(time.RFC3339),
			},
		})
	}
}

// accountDataResponse is the JSON shape for GDPR data export.
type accountDataResponse struct {
	Customer accountCustomer  `json:"customer"`
	Orders   []accountOrder   `json:"orders"`
	Consent  *consentResponse `json:"consent,omitempty"`
}

type accountCustomer struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	CreatedAt string `json:"created_at"`
}

type accountOrder struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Currency    string `json:"currency"`
	TotalAmount int64  `json:"total_amount"`
	CreatedAt   string `json:"created_at"`
}

// GetData handles GET /api/v1/account/data — returns all personal data.
func (h *AccountHandler) GetData() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID

		cust, err := h.customers.FindByID(r.Context(), customerID)
		if err != nil {
			JSONError(w, err)
			return
		}
		if cust == nil {
			JSONError(w, apperror.NotFound("account not found"))
			return
		}

		orders, err := h.orders.FindByCustomerID(r.Context(), customerID)
		if err != nil {
			JSONError(w, err)
			return
		}

		consent, err := h.consents.FindByCustomerID(r.Context(), customerID)
		if err != nil {
			JSONError(w, err)
			return
		}

		resp := accountDataResponse{
			Customer: accountCustomer{
				ID:        cust.ID,
				Email:     cust.Email,
				FirstName: cust.FirstName,
				LastName:  cust.LastName,
				CreatedAt: cust.CreatedAt.UTC().Format(time.RFC3339),
			},
			Orders: toAccountOrders(orders),
		}
		if consent != nil {
			resp.Consent = &consentResponse{
				Necessary: consent.Necessary,
				Analytics: consent.Analytics,
				Marketing: consent.Marketing,
				UpdatedAt: consent.UpdatedAt.UTC().Format(time.RFC3339),
			}
		} else {
			resp.Consent = &consentResponse{
				Necessary: true,
				Analytics: false,
				Marketing: false,
			}
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"account": resp,
		})
	}
}

// Export handles GET /api/v1/account/export — same as GetData with Content-Disposition header.
func (h *AccountHandler) Export() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", "attachment; filename=\"account-data.json\"")
		h.GetData()(w, r)
	}
}

// Delete handles DELETE /api/v1/account — deletes the account via application service.
func (h *AccountHandler) Delete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID

		if err := h.deleter.DeleteAccount(r.Context(), customerID); err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"message": "account deleted",
		})
	}
}

func toAccountOrders(orders []order.Order) []accountOrder {
	result := make([]accountOrder, 0, len(orders))
	for i := range orders {
		result = append(result, accountOrder{
			ID:          orders[i].ID,
			Status:      string(orders[i].Status()),
			Currency:    orders[i].Currency,
			TotalAmount: orders[i].TotalAmount.Amount(),
			CreatedAt:   orders[i].CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return result
}
