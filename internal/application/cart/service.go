package cart

import (
	"context"
	"fmt"
	"time"

	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/promotion"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// Service orchestrates cart use cases.
type Service struct {
	carts      domainCart.CartRepository
	prices     pricing.PriceRepository
	promotions promotion.PromotionRepository
	coupons    promotion.CouponRepository
	pipeline   pricing.Pipeline
	log        logger.Logger
	bus        *event.Bus
}

// NewService creates a cart application service.
func NewService(
	carts domainCart.CartRepository,
	prices pricing.PriceRepository,
	promotions promotion.PromotionRepository,
	coupons promotion.CouponRepository,
	pipeline pricing.Pipeline,
	log logger.Logger,
	bus *event.Bus,
) *Service {
	return &Service{
		carts:      carts,
		prices:     prices,
		promotions: promotions,
		coupons:    coupons,
		pipeline:   pipeline,
		log:        log,
		bus:        bus,
	}
}

// CreateCart creates a new active cart with the given currency, owned by customerID.
func (s *Service) CreateCart(ctx context.Context, customerID, currency string) (*domainCart.Cart, error) {
	c, err := domainCart.NewCart(id.New(), currency)
	if err != nil {
		return nil, apperror.Wrap(apperror.CodeValidation, "invalid cart parameters", err)
	}
	if err := c.SetCustomerID(customerID); err != nil {
		return nil, apperror.Wrap(apperror.CodeValidation, "invalid customer id", err)
	}
	if err := s.carts.Save(ctx, &c); err != nil {
		return nil, fmt.Errorf("cart service: create: %w", err)
	}
	s.log.Info("cart.created", map[string]interface{}{
		"cart_id":     c.ID,
		"customer_id": customerID,
		"currency":    c.Currency,
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
func (s *Service) AddItem(ctx context.Context, cartID, customerID, variantID string, quantity int) (*domainCart.Cart, error) {
	c, err := s.carts.FindByID(ctx, cartID)
	if err != nil {
		return nil, fmt.Errorf("cart service: add item: find: %w", err)
	}
	if c == nil {
		return nil, apperror.NotFound("cart not found")
	}
	if c.CustomerID != customerID {
		return nil, apperror.Forbidden("cannot modify another customer's cart")
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
	_ = s.bus.Publish(ctx, event.New(domainCart.EventItemAdded, "cart.service", domainCart.ItemAddedData{
		CartID:    c.ID,
		VariantID: variantID,
		Quantity:  quantity,
	}))
	return c, nil
}

// UpdateItemQuantity sets the quantity of an existing item and recalculates pricing.
func (s *Service) UpdateItemQuantity(ctx context.Context, cartID, customerID, variantID string, quantity int) (*domainCart.Cart, error) {
	c, err := s.carts.FindByID(ctx, cartID)
	if err != nil {
		return nil, fmt.Errorf("cart service: update item: find: %w", err)
	}
	if c == nil {
		return nil, apperror.NotFound("cart not found")
	}
	if c.CustomerID != customerID {
		return nil, apperror.Forbidden("cannot modify another customer's cart")
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
	_ = s.bus.Publish(ctx, event.New(domainCart.EventItemUpdated, "cart.service", domainCart.ItemUpdatedData{
		CartID:    c.ID,
		VariantID: variantID,
		Quantity:  quantity,
	}))
	return c, nil
}

// RemoveItem removes an item from the cart and recalculates pricing.
func (s *Service) RemoveItem(ctx context.Context, cartID, customerID, variantID string) (*domainCart.Cart, error) {
	c, err := s.carts.FindByID(ctx, cartID)
	if err != nil {
		return nil, fmt.Errorf("cart service: remove item: find: %w", err)
	}
	if c == nil {
		return nil, apperror.NotFound("cart not found")
	}
	if c.CustomerID != customerID {
		return nil, apperror.Forbidden("cannot modify another customer's cart")
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
	_ = s.bus.Publish(ctx, event.New(domainCart.EventItemRemoved, "cart.service", domainCart.ItemRemovedData{
		CartID:    c.ID,
		VariantID: variantID,
	}))
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

	if c.CouponCode != "" {
		pctx.Meta["coupon_code"] = c.CouponCode
	}

	// Propagate store_id so pricing steps can scope lookups.
	if s := store.FromContext(ctx); s != nil {
		pctx.Meta["store_id"] = s.ID
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

// lookupPrice fetches the base price for a variant in the cart's currency,
// scoped to the current store when available.
func (s *Service) lookupPrice(ctx context.Context, variantID, currency string) (shared.Money, error) {
	var storeID string
	if st := store.FromContext(ctx); st != nil {
		storeID = st.ID
	}
	p, err := s.prices.FindByVariantCurrencyAndStore(ctx, variantID, currency, storeID)
	if err != nil {
		return shared.Money{}, fmt.Errorf("cart service: lookup price: %w", err)
	}
	// Fall back to global price when store-scoped price not found.
	if p == nil && storeID != "" {
		p, err = s.prices.FindByVariantCurrencyAndStore(ctx, variantID, currency, "")
		if err != nil {
			return shared.Money{}, fmt.Errorf("cart service: lookup price (fallback): %w", err)
		}
	}
	if p == nil {
		return shared.Money{}, apperror.Validation(
			fmt.Sprintf("no price for variant %s in %s", variantID, currency),
		)
	}
	return p.Amount, nil
}

// ApplyCoupon validates a coupon code, associates it with the cart, and
// recalculates pricing so promotion discounts take effect.
func (s *Service) ApplyCoupon(ctx context.Context, cartID, customerID, code string) (*domainCart.Cart, error) {
	c, err := s.carts.FindByID(ctx, cartID)
	if err != nil {
		return nil, fmt.Errorf("cart service: apply coupon: find: %w", err)
	}
	if c == nil {
		return nil, apperror.NotFound("cart not found")
	}
	if c.CustomerID != customerID {
		return nil, apperror.Forbidden("cannot modify another customer's cart")
	}

	coupon, err := s.coupons.FindByCode(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("cart service: apply coupon: lookup: %w", err)
	}
	if coupon == nil {
		return nil, apperror.Validation("coupon not found")
	}
	if !coupon.CanRedeem() {
		return nil, apperror.Validation("coupon is not redeemable")
	}

	promo, err := s.promotions.FindByID(ctx, coupon.PromotionID)
	if err != nil {
		return nil, fmt.Errorf("cart service: apply coupon: find promotion: %w", err)
	}
	if promo == nil || !promo.IsEligible(timeNow()) {
		return nil, apperror.Validation("promotion is not active")
	}
	if promo.Type != promotion.TypeCatalog || !promo.CouponBound {
		return nil, apperror.Validation("promotion not applicable for coupon")
	}

	if err := c.ApplyCoupon(code); err != nil {
		return nil, apperror.Wrap(apperror.CodeValidation, "cannot apply coupon", err)
	}

	if err := s.recalculate(ctx, c); err != nil {
		return nil, err
	}

	if err := s.carts.Save(ctx, c); err != nil {
		return nil, fmt.Errorf("cart service: apply coupon: save: %w", err)
	}

	s.log.Info("cart.coupon.applied", map[string]interface{}{
		"cart_id":     c.ID,
		"coupon_code": code,
	})
	_ = s.bus.Publish(ctx, event.New(domainCart.EventCouponApplied, "cart.service", domainCart.CouponAppliedData{
		CartID:     c.ID,
		CouponCode: code,
	}))
	return c, nil
}

// RemoveCoupon removes the applied coupon from the cart and recalculates pricing.
func (s *Service) RemoveCoupon(ctx context.Context, cartID, customerID string) (*domainCart.Cart, error) {
	c, err := s.carts.FindByID(ctx, cartID)
	if err != nil {
		return nil, fmt.Errorf("cart service: remove coupon: find: %w", err)
	}
	if c == nil {
		return nil, apperror.NotFound("cart not found")
	}
	if c.CustomerID != customerID {
		return nil, apperror.Forbidden("cannot modify another customer's cart")
	}

	previousCode := c.CouponCode

	if err := c.RemoveCoupon(); err != nil {
		return nil, apperror.Wrap(apperror.CodeValidation, "cannot remove coupon", err)
	}

	if err := s.recalculate(ctx, c); err != nil {
		return nil, err
	}

	if err := s.carts.Save(ctx, c); err != nil {
		return nil, fmt.Errorf("cart service: remove coupon: save: %w", err)
	}

	s.log.Info("cart.coupon.removed", map[string]interface{}{
		"cart_id": c.ID,
	})
	_ = s.bus.Publish(ctx, event.New(domainCart.EventCouponRemoved, "cart.service", domainCart.CouponRemovedData{
		CartID:     c.ID,
		CouponCode: previousCode,
	}))
	return c, nil
}

// timeNow is a package-level function for testing seams.
var timeNow = func() time.Time { return time.Now() }
