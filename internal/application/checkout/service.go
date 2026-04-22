package checkout

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/cart"
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
	if customerID == "" {
		return nil, apperror.Validation("customer id must not be empty")
	}
	input.Address = input.Address.Normalize()
	if !input.Address.IsZero() {
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
	if c.ItemCount() == 0 {
		return nil, apperror.Validation("cart is empty")
	}

	cctx := NewContext(cartID, customerID, c.Currency)
	cctx.Cart = c
	cctx.Input = input
	cctx.SetMeta("checkout_address", input.Address)
	cctx.SetMeta("checkout_shipping_method", input.ShippingMethod)
	cctx.SetMeta("checkout_payment_method", input.PaymentMethod)

	s.log.Info("checkout.started", map[string]interface{}{
		"cart_id":     cartID,
		"customer_id": customerID,
		"items":       c.ItemCount(),
	})

	if err := s.workflow.Execute(ctx, cctx); err != nil {
		return cctx, err
	}

	return cctx, nil
}
