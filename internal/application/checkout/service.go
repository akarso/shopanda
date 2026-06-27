package checkout

import (
	"context"
	"fmt"
	"net/mail"
	"strings"

	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/domain/tax"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// Service orchestrates the checkout workflow.
type Service struct {
	carts    cart.CartRepository
	workflow *Workflow
	log      logger.Logger
}

// NewService creates a checkout application service.
func NewService(
	carts cart.CartRepository,
	workflow *Workflow,
	log logger.Logger,
) *Service {
	if carts == nil {
		panic("checkout: carts must not be nil")
	}
	if workflow == nil {
		panic("checkout: workflow must not be nil")
	}
	if log == nil {
		panic("checkout: logger must not be nil")
	}
	return &Service{
		carts:    carts,
		workflow: workflow,
		log:      log,
	}
}

// StartCheckout loads the cart, validates it, and runs the checkout workflow.
func (s *Service) StartCheckout(ctx context.Context, cartID, customerID string, input Input) (*Context, error) {
	if cartID == "" {
		return nil, apperror.Validation("cart id must not be empty")
	}
	input.ContactEmail = strings.ToLower(strings.TrimSpace(input.ContactEmail))
	input.Address = input.Address.Normalize()
	if input.Address.IsZero() {
		return nil, apperror.Validation("address is required")
	}
	switch {
	case input.Address.FirstName == "":
		return nil, apperror.Validation("first name is required")
	case input.Address.LastName == "":
		return nil, apperror.Validation("last name is required")
	case input.Address.Street == "":
		return nil, apperror.Validation("street is required")
	case input.Address.City == "":
		return nil, apperror.Validation("city is required")
	case input.Address.Postcode == "":
		return nil, apperror.Validation("postcode is required")
	case input.Address.Country == "":
		return nil, apperror.Validation("country is required")
	}

	c, err := s.carts.FindByID(ctx, cartID)
	if err != nil {
		return nil, fmt.Errorf("checkout: find cart: %w", err)
	}
	if c == nil {
		return nil, apperror.NotFound("cart not found")
	}
	if !c.IsActive() {
		return nil, apperror.Validation("cart is not active")
	}
	if c.CustomerID != customerID {
		return nil, apperror.Forbidden("cart does not belong to this customer")
	}
	if customerID == "" {
		if input.ContactEmail == "" {
			return nil, apperror.Validation("contact email is required for guest checkout")
		}
		if _, err := mail.ParseAddress(input.ContactEmail); err != nil {
			return nil, apperror.Validation("contact email is invalid")
		}
	} else if input.ContactEmail != "" {
		if _, err := mail.ParseAddress(input.ContactEmail); err != nil {
			return nil, apperror.Validation("contact email is invalid")
		}
	}
	if c.ItemCount() == 0 {
		return nil, apperror.Validation("cart is empty")
	}

	cctx := NewContext(cartID, customerID, c.Currency)
	cctx.Cart = c
	cctx.Input = input
	cctx.SetMeta("checkout_address", input.Address)
	cctx.SetMeta("checkout_contact_email", input.ContactEmail)
	cctx.SetMeta("checkout_shipping_method", input.ShippingMethod)
	cctx.SetMeta("checkout_payment_method", input.PaymentMethod)

	country := strings.ToUpper(strings.TrimSpace(input.Address.Country))
	if len(country) == 2 {
		cctx.SetMeta("tax_country", country)
		cctx.SetMeta("tax_mode", string(tax.ModeExclusive))
	}
	if st := store.FromContext(ctx); st != nil {
		cctx.SetMeta("store_id", st.ID)
	}

	s.log.Info("checkout.started", map[string]interface{}{
		"cart_id":           cartID,
		"customer_id":       customerID,
		"guest":             customerID == "",
		"has_contact_email": input.ContactEmail != "",
		"items":             c.ItemCount(),
	})

	if err := s.workflow.Execute(ctx, cctx); err != nil {
		return cctx, err
	}

	return cctx, nil
}
