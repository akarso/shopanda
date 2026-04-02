package cart

import (
	"context"
	"fmt"

	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// Service orchestrates cart use cases.
type Service struct {
	carts    domainCart.CartRepository
	prices   pricing.PriceRepository
	pipeline pricing.Pipeline
	log      logger.Logger
}

// NewService creates a cart application service.
func NewService(
	carts domainCart.CartRepository,
	prices pricing.PriceRepository,
	pipeline pricing.Pipeline,
	log logger.Logger,
) *Service {
	return &Service{
		carts:    carts,
		prices:   prices,
		pipeline: pipeline,
		log:      log,
	}
}

// CreateCart creates a new active cart with the given currency.
func (s *Service) CreateCart(ctx context.Context, currency string) (*domainCart.Cart, error) {
	c, err := domainCart.NewCart(id.New(), currency)
	if err != nil {
		return nil, apperror.Wrap(apperror.CodeValidation, "invalid cart parameters", err)
	}
	if err := s.carts.Save(ctx, &c); err != nil {
		return nil, fmt.Errorf("cart service: create: %w", err)
	}
	s.log.Info("cart.created", map[string]interface{}{
		"cart_id":  c.ID,
		"currency": c.Currency,
	})
	return &c, nil
}

// GetCart returns a cart by ID.
func (s *Service) GetCart(ctx context.Context, cartID string) (*domainCart.Cart, error) {
	c, err := s.carts.FindByID(ctx, cartID)
	if err != nil {
		return nil, fmt.Errorf("cart service: get: %w", err)
	}
	if c == nil {
		return nil, apperror.NotFound("cart not found")
	}
	return c, nil
}

// GetActiveCartByCustomer returns the active cart for a customer.
func (s *Service) GetActiveCartByCustomer(ctx context.Context, customerID string) (*domainCart.Cart, error) {
	c, err := s.carts.FindActiveByCustomerID(ctx, customerID)
	if err != nil {
		return nil, fmt.Errorf("cart service: get active: %w", err)
	}
	if c == nil {
		return nil, apperror.NotFound("no active cart for customer")
	}
	return c, nil
}

// AddItem adds an item to the cart and recalculates pricing.
func (s *Service) AddItem(ctx context.Context, cartID, variantID string, quantity int) (*domainCart.Cart, error) {
	c, err := s.carts.FindByID(ctx, cartID)
	if err != nil {
		return nil, fmt.Errorf("cart service: add item: find: %w", err)
	}
	if c == nil {
		return nil, apperror.NotFound("cart not found")
	}

	price, err := s.lookupPrice(ctx, variantID, c.Currency)
	if err != nil {
		return nil, err
	}

	if err := c.AddItem(variantID, quantity, price); err != nil {
		return nil, apperror.Wrap(apperror.CodeValidation, "cannot add item", err)
	}

	if err := s.recalculate(ctx, c); err != nil {
		return nil, err
	}

	if err := s.carts.Save(ctx, c); err != nil {
		return nil, fmt.Errorf("cart service: add item: save: %w", err)
	}

	s.log.Info("cart.item.added", map[string]interface{}{
		"cart_id":    c.ID,
		"variant_id": variantID,
		"quantity":   quantity,
	})
	return c, nil
}

// UpdateItemQuantity sets the quantity of an existing item and recalculates pricing.
func (s *Service) UpdateItemQuantity(ctx context.Context, cartID, variantID string, quantity int) (*domainCart.Cart, error) {
	c, err := s.carts.FindByID(ctx, cartID)
	if err != nil {
		return nil, fmt.Errorf("cart service: update item: find: %w", err)
	}
	if c == nil {
		return nil, apperror.NotFound("cart not found")
	}

	if err := c.UpdateItemQuantity(variantID, quantity); err != nil {
		return nil, apperror.Wrap(apperror.CodeValidation, "cannot update item", err)
	}

	if err := s.recalculate(ctx, c); err != nil {
		return nil, err
	}

	if err := s.carts.Save(ctx, c); err != nil {
		return nil, fmt.Errorf("cart service: update item: save: %w", err)
	}

	s.log.Info("cart.item.updated", map[string]interface{}{
		"cart_id":    c.ID,
		"variant_id": variantID,
		"quantity":   quantity,
	})
	return c, nil
}

// RemoveItem removes an item from the cart and recalculates pricing.
func (s *Service) RemoveItem(ctx context.Context, cartID, variantID string) (*domainCart.Cart, error) {
	c, err := s.carts.FindByID(ctx, cartID)
	if err != nil {
		return nil, fmt.Errorf("cart service: remove item: find: %w", err)
	}
	if c == nil {
		return nil, apperror.NotFound("cart not found")
	}

	if err := c.RemoveItem(variantID); err != nil {
		return nil, apperror.Wrap(apperror.CodeValidation, "cannot remove item", err)
	}

	if err := s.recalculate(ctx, c); err != nil {
		return nil, err
	}

	if err := s.carts.Save(ctx, c); err != nil {
		return nil, fmt.Errorf("cart service: remove item: save: %w", err)
	}

	s.log.Info("cart.item.removed", map[string]interface{}{
		"cart_id":    c.ID,
		"variant_id": variantID,
	})
	return c, nil
}

// recalculate runs the pricing pipeline over the cart's items and writes
// the computed prices back onto the cart.
func (s *Service) recalculate(ctx context.Context, c *domainCart.Cart) error {
	if len(c.Items) == 0 {
		return nil
	}

	pctx, err := pricing.NewPricingContext(c.Currency)
	if err != nil {
		return fmt.Errorf("cart service: pricing context: %w", err)
	}

	for _, item := range c.Items {
		pi, err := pricing.NewPricingItem(item.VariantID, item.Quantity, item.UnitPrice)
		if err != nil {
			return fmt.Errorf("cart service: pricing item %q: %w", item.VariantID, err)
		}
		pctx.Items = append(pctx.Items, pi)
	}

	if err := s.pipeline.Execute(ctx, &pctx); err != nil {
		return fmt.Errorf("cart service: pricing pipeline: %w", err)
	}

	// Write back the computed unit prices, matching by VariantID.
	for _, pi := range pctx.Items {
		for j := range c.Items {
			if c.Items[j].VariantID == pi.VariantID {
				c.Items[j].UnitPrice = pi.UnitPrice
				break
			}
		}
	}

	return nil
}

// lookupPrice fetches the base price for a variant in the cart's currency.
func (s *Service) lookupPrice(ctx context.Context, variantID, currency string) (shared.Money, error) {
	p, err := s.prices.FindByVariantAndCurrency(ctx, variantID, currency)
	if err != nil {
		return shared.Money{}, fmt.Errorf("cart service: lookup price: %w", err)
	}
	if p == nil {
		return shared.Money{}, apperror.Validation(
			fmt.Sprintf("no price for variant %s in %s", variantID, currency),
		)
	}
	return p.Amount, nil
}
