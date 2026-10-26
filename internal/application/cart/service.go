package cart

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	catalogapp "github.com/akarso/shopanda/internal/application/catalog"
	"github.com/akarso/shopanda/internal/application/hooks"
	appinventory "github.com/akarso/shopanda/internal/application/inventory"
	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/promotion"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/domain/tax"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// AddItemOptions carries optional extension values for a cart line.
type AddItemOptions struct {
	Extensions []domainext.ValueInput
	UpdatedBy  string
}

// Service orchestrates cart use cases.
type Service struct {
	carts           domainCart.CartRepository
	prices          pricing.PriceRepository
	promotions      promotion.PromotionRepository
	coupons         promotion.CouponRepository
	variants        catalog.VariantRepository
	products        catalog.ProductRepository
	stock           inventory.StockRepository
	unitPrices      appPricing.UnitPriceSources
	visibilityFacts catalog.VisibilityFactsSource
	visibilityOpts  catalog.VisibilityOptions
	pipeline        pricing.Pipeline
	extensions      extensionValueWriter
	hooks           *hooks.Registry
	log             logger.Logger
	bus             *event.Bus
}

type extensionValueWriter interface {
	ValidateBatch(ctx context.Context, target domainext.Target, inputs []domainext.ValueInput, canAccessPrivate bool) error
	UpsertBatch(ctx context.Context, target domainext.Target, inputs []domainext.ValueInput, updatedBy string, canAccessPrivate bool) ([]domainext.Value, error)
	DeleteAllForTarget(ctx context.Context, target domainext.Target) error
	DeleteOrphanCartItemExtensions(ctx context.Context, cartID string, keepVariantIDs []string) error
	CopyTarget(ctx context.Context, from, to domainext.Target, updatedBy string) error
}

// NewService creates a cart application service.
// components is required: dynamic bundle prices are resolved from it, and a
// nil source would let production forget the rollup. prices, variants,
// products, and stock are required for the same reason. stock caps a line
// to ResolveAvailableQty.
func NewService(
	carts domainCart.CartRepository,
	prices pricing.PriceRepository,
	promotions promotion.PromotionRepository,
	coupons promotion.CouponRepository,
	variants catalog.VariantRepository,
	products catalog.ProductRepository,
	components catalog.BundleComponentRepository,
	stock inventory.StockRepository,
	pipeline pricing.Pipeline,
	log logger.Logger,
	bus *event.Bus,
	extensions extensionValueWriter,
	hookRegistry *hooks.Registry,
) *Service {
	if prices == nil || variants == nil || products == nil || components == nil || stock == nil {
		panic("cart: unit price sources require prices, variants, products, bundle components, and stock")
	}
	return &Service{
		carts:      carts,
		prices:     prices,
		promotions: promotions,
		coupons:    coupons,
		variants:   variants,
		products:   products,
		stock:      stock,
		unitPrices: appPricing.UnitPriceSources{
			Prices:     prices,
			Variants:   variants,
			Products:   products,
			Components: components,
		},
		pipeline:   pipeline,
		extensions: extensions,
		hooks:      hookRegistry,
		log:        log,
		bus:        bus,
	}
}

// WithVisibilityFacts enables the four-axis Purchasable check on top of
// ActiveForPurchase. Nil facts leaves the axis gate off (catalog.visibility.enforce false).
func (s *Service) WithVisibilityFacts(facts catalog.VisibilityFactsSource, opts catalog.VisibilityOptions) *Service {
	s.visibilityFacts = facts
	s.visibilityOpts = opts
	return s
}

func (s *Service) priceSources() appPricing.UnitPriceSources {
	return s.unitPrices
}

func (s *Service) stockSources() appinventory.AvailableQtySources {
	return appinventory.AvailableQtySources{
		Stock:      s.stock,
		Variants:   s.variants,
		Products:   s.products,
		Components: s.unitPrices.Components,
	}
}

// CreateCart creates a new active cart with the given currency, owned by customerID.
func (s *Service) CreateCart(ctx context.Context, customerID, currency string) (*domainCart.Cart, error) {
	c, err := domainCart.NewCart(id.New(), currency)
	if err != nil {
		return nil, apperror.Wrap(apperror.CodeValidation, "invalid cart parameters", err)
	}
	if customerID != "" {
		if err := c.SetCustomerID(customerID); err != nil {
			return nil, apperror.Wrap(apperror.CodeValidation, "invalid customer id", err)
		}
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

// DiscardEmptyGuestCart deletes a guest cart that still has no lines and
// purges leftover cart_item extension rows for that cart ID. Customer carts
// and carts with items are left untouched.
func (s *Service) DiscardEmptyGuestCart(ctx context.Context, cartID string) error {
	cartID = strings.TrimSpace(cartID)
	if cartID == "" {
		return nil
	}
	c, err := s.carts.FindByID(ctx, cartID)
	if err != nil {
		return fmt.Errorf("cart service: discard empty guest: find: %w", err)
	}
	if c == nil || c.CustomerID != "" || c.ItemCount() > 0 {
		return nil
	}
	if s.extensions != nil {
		if err := s.extensions.DeleteOrphanCartItemExtensions(ctx, cartID, nil); err != nil {
			return fmt.Errorf("cart service: discard empty guest: purge extensions: %w", err)
		}
	}
	if err := s.carts.Delete(ctx, cartID); err != nil {
		return fmt.Errorf("cart service: discard empty guest: delete: %w", err)
	}
	return nil
}

// GetCart returns a cart by ID when it belongs to customerID.
// Guest callers pass an empty customerID and may only read guest-owned carts.
func (s *Service) GetCart(ctx context.Context, cartID, customerID string) (*domainCart.Cart, error) {
	c, err := s.carts.FindByID(ctx, cartID)
	if err != nil {
		return nil, fmt.Errorf("cart service: get: %w", err)
	}
	if c == nil {
		return nil, apperror.NotFound("cart not found")
	}
	if c.CustomerID != customerID {
		return nil, apperror.Forbidden("cannot access another customer's cart")
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

// ClaimGuestCart assigns a guest cart to a customer or merges it into the
// customer's active cart when one already exists.
func (s *Service) ClaimGuestCart(ctx context.Context, guestCartID, customerID string) (*domainCart.Cart, error) {
	if guestCartID == "" {
		return nil, nil
	}
	if customerID == "" {
		return nil, apperror.Validation("customer id must not be empty")
	}

	guestCart, err := s.carts.FindByID(ctx, guestCartID)
	if err != nil {
		return nil, fmt.Errorf("cart service: claim guest cart: find guest: %w", err)
	}
	if guestCart == nil {
		return nil, nil
	}
	if guestCart.CustomerID != "" {
		if guestCart.CustomerID == customerID {
			if err := s.finishAssignedGuestCleanup(ctx, guestCart); err != nil {
				return nil, err
			}
			return guestCart, nil
		}
		return nil, nil
	}

	customerCart, err := s.carts.FindActiveByCustomerID(ctx, customerID)
	if err != nil {
		return nil, fmt.Errorf("cart service: claim guest cart: find customer cart: %w", err)
	}
	if customerCart != nil && customerCart.MergedGuestID == guestCart.ID {
		if err := s.finishMergedGuestCleanup(ctx, guestCart.ID); err != nil {
			return nil, err
		}
		return customerCart, nil
	}
	if customerCart == nil {
		return s.assignGuestCart(ctx, guestCart, customerID)
	}

	if customerCart.Currency != guestCart.Currency {
		if customerCart.ItemCount() == 0 {
			if err := s.carts.Delete(ctx, customerCart.ID); err != nil {
				return nil, fmt.Errorf("cart service: claim guest cart: delete empty customer cart: %w", err)
			}
			return s.assignGuestCart(ctx, guestCart, customerID)
		}
		if guestCart.ItemCount() == 0 {
			if err := s.carts.Delete(ctx, guestCart.ID); err != nil {
				return nil, fmt.Errorf("cart service: claim guest cart: delete empty guest cart: %w", err)
			}
			return customerCart, nil
		}
		return nil, apperror.Validation("cannot merge carts with different currencies")
	}

	return s.mergeIntoCustomerCart(ctx, guestCart, customerCart)
}

// unclaimableGuestLines checks every guest line before any cart or extension
// mutation. into is the customer cart when merging and nil when assigning.
// The check uses the quantity the line would have after the claim, so a
// merge that adds guest units onto an existing customer line is dropped
// when that total exceeds available quantity. Lines whose variant is gone,
// whose product is no longer purchasable, or whose resulting quantity is
// above available are returned so the claim can drop them instead of
// failing: a failed claim leaves the guest cookie in place and every later
// login retries and fails again. Infrastructure errors still abort the claim.
func (s *Service) unclaimableGuestLines(ctx context.Context, guestCart, into *domainCart.Cart) (map[string]struct{}, error) {
	dropped := make(map[string]struct{})
	for _, item := range guestCart.Items {
		lineQty, qtyErr := guestLineQtyForClaim(into, item.VariantID, item.Quantity)
		if qtyErr != nil {
			if apperror.Is(qtyErr, apperror.CodeValidation) {
				dropped[item.VariantID] = struct{}{}
				continue
			}
			return nil, qtyErr
		}
		err := s.ensureVariantPurchasable(ctx, item.VariantID, guestCart.Currency, lineQty, lineAssignment(item))
		if err == nil {
			continue
		}
		if apperror.Is(err, apperror.CodeValidation) || apperror.Is(err, apperror.CodeNotFound) {
			dropped[item.VariantID] = struct{}{}
			continue
		}
		return nil, err
	}
	if len(dropped) > 0 {
		variantIDs := make([]string, 0, len(dropped))
		for _, item := range guestCart.Items {
			if _, ok := dropped[item.VariantID]; ok {
				variantIDs = append(variantIDs, item.VariantID)
			}
		}
		s.log.Warn("cart.guest.claim.lines_dropped", map[string]interface{}{
			"guest_cart_id": guestCart.ID,
			"variant_ids":   variantIDs,
		})
	}
	return dropped, nil
}

func (s *Service) deleteGuestLineExtensions(ctx context.Context, guestCartID, variantID string) error {
	if s.extensions == nil {
		return nil
	}
	if err := s.extensions.DeleteAllForTarget(ctx, domainext.CartItemTarget(guestCartID, variantID)); err != nil {
		return fmt.Errorf("cart service: claim guest cart: delete guest extensions: %w", err)
	}
	return nil
}

func (s *Service) finishAssignedGuestCleanup(ctx context.Context, cart *domainCart.Cart) error {
	if s.extensions == nil || cart == nil {
		return nil
	}
	keepVariantIDs := make([]string, 0, len(cart.Items))
	for _, item := range cart.Items {
		keepVariantIDs = append(keepVariantIDs, item.VariantID)
	}
	if err := s.extensions.DeleteOrphanCartItemExtensions(ctx, cart.ID, keepVariantIDs); err != nil {
		return fmt.Errorf("cart service: claim guest cart: purge orphan extensions: %w", err)
	}
	return nil
}

func (s *Service) finishMergedGuestCleanup(ctx context.Context, guestCartID string) error {
	guestCart, err := s.carts.FindByID(ctx, guestCartID)
	if err != nil {
		return fmt.Errorf("cart service: claim guest cart: finish merge cleanup find guest: %w", err)
	}
	if guestCart == nil {
		return nil
	}
	for _, item := range guestCart.Items {
		if err := s.deleteGuestLineExtensions(ctx, guestCartID, item.VariantID); err != nil {
			return err
		}
	}
	if err := s.carts.Delete(ctx, guestCartID); err != nil {
		if apperror.Is(err, apperror.CodeNotFound) {
			return nil
		}
		return fmt.Errorf("cart service: claim guest cart: delete guest cart: %w", err)
	}
	return nil
}

func (s *Service) assignGuestCart(ctx context.Context, guestCart *domainCart.Cart, customerID string) (*domainCart.Cart, error) {
	dropped, err := s.unclaimableGuestLines(ctx, guestCart, nil)
	if err != nil {
		return nil, err
	}
	// Drop lines in memory first; delete their extension rows only after Save
	// succeeds. Extension deletes autocommit separately from cart Save — doing
	// them earlier would leave the DB cart unchanged but wipe custom values if
	// recalculate/Save fails (retry would then find the line without extensions).
	for variantID := range dropped {
		if err := guestCart.RemoveItem(variantID); err != nil {
			return nil, apperror.Wrap(apperror.CodeValidation, "cannot drop unavailable guest cart item", err)
		}
	}
	if err := guestCart.SetCustomerID(customerID); err != nil {
		return nil, apperror.Wrap(apperror.CodeValidation, "cannot assign guest cart", err)
	}
	if err := s.recalculate(ctx, guestCart); err != nil {
		return nil, err
	}
	if err := s.carts.Save(ctx, guestCart); err != nil {
		return nil, fmt.Errorf("cart service: claim guest cart: save claimed cart: %w", err)
	}
	if err := s.finishAssignedGuestCleanup(ctx, guestCart); err != nil {
		return nil, err
	}
	s.log.Info("cart.guest.claimed", map[string]interface{}{
		"cart_id":     guestCart.ID,
		"customer_id": customerID,
	})
	return guestCart, nil
}

func (s *Service) mergeIntoCustomerCart(ctx context.Context, guestCart, customerCart *domainCart.Cart) (*domainCart.Cart, error) {
	// Validate every guest line before copying extensions so a late failure
	// cannot leave earlier guest targets half-migrated. Guest extension deletes
	// wait until after customer cart Save so a failed Save leaves guest values
	// intact (same ordering as assignGuestCart).
	dropped, err := s.unclaimableGuestLines(ctx, guestCart, customerCart)
	if err != nil {
		return nil, err
	}
	for _, item := range guestCart.Items {
		if _, skip := dropped[item.VariantID]; skip {
			continue
		}
		if assignmentsConflict(lineAssignmentFromCart(customerCart, item.VariantID), lineAssignment(item)) {
			return nil, apperror.Validation(conflictingLinkedParentAssignmentMessage)
		}
		if err := customerCart.AddItem(item.VariantID, item.Quantity, item.UnitPrice); err != nil {
			return nil, apperror.Wrap(apperror.CodeValidation, "cannot merge guest cart item", err)
		}
		stampLineAssignment(customerCart, item.VariantID, lineAssignment(item))
		if err := s.copyCartItemExtensions(ctx, guestCart.ID, customerCart.ID, item.VariantID); err != nil {
			return nil, err
		}
	}
	if err := s.applyEligibleGuestCoupon(ctx, customerCart, guestCart.CouponCode); err != nil {
		return nil, err
	}
	customerCart.MergedGuestID = guestCart.ID
	if err := s.recalculate(ctx, customerCart); err != nil {
		return nil, err
	}
	if err := s.carts.Save(ctx, customerCart); err != nil {
		return nil, fmt.Errorf("cart service: claim guest cart: save merged cart: %w", err)
	}
	if err := s.finishMergedGuestCleanup(ctx, guestCart.ID); err != nil {
		return nil, err
	}

	s.log.Info("cart.guest.merged", map[string]interface{}{
		"guest_cart_id":    guestCart.ID,
		"customer_cart_id": customerCart.ID,
		"customer_id":      customerCart.CustomerID,
	})
	return customerCart, nil
}

func (s *Service) applyEligibleGuestCoupon(ctx context.Context, customerCart *domainCart.Cart, code string) error {
	if customerCart.CouponCode != "" || code == "" || s.coupons == nil || s.promotions == nil {
		return nil
	}
	coupon, err := s.coupons.FindByCode(ctx, code)
	if err != nil {
		return fmt.Errorf("cart service: claim guest cart: lookup coupon: %w", err)
	}
	if coupon == nil || !coupon.CanRedeem() {
		return nil
	}
	promo, err := s.promotions.FindByID(ctx, coupon.PromotionID)
	if err != nil {
		return fmt.Errorf("cart service: claim guest cart: lookup promotion: %w", err)
	}
	if promo == nil || !promo.IsEligible(timeNow()) || promo.Type != promotion.TypeCatalog || !promo.CouponBound {
		return nil
	}
	if err := customerCart.ApplyCoupon(code); err != nil {
		return apperror.Wrap(apperror.CodeValidation, "cannot merge guest cart coupon", err)
	}
	return nil
}

// AddItem adds an item to the cart and recalculates pricing.
func (s *Service) AddItem(ctx context.Context, cartID, customerID, variantID string, quantity int, opts AddItemOptions) (*domainCart.Cart, error) {
	ctx = appPricing.WithUnitPriceCache(ctx)
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

	lineQty, err := resultingLineQty(c, variantID, quantity)
	if err != nil {
		return nil, err
	}
	if err := s.ensureVariantPurchasable(ctx, variantID, c.Currency, lineQty, lineAssignmentFromCart(c, variantID)); err != nil {
		return nil, err
	}
	if assignmentsConflict(lineAssignmentFromCart(c, variantID), catalog.AssignmentContextFrom(ctx)) {
		return nil, apperror.Validation(conflictingLinkedParentAssignmentMessage)
	}

	if len(opts.Extensions) > 0 {
		if s.extensions == nil {
			return nil, apperror.Validation("extension values are not supported")
		}
		target := domainext.CartItemTarget(cartID, variantID)
		if err := s.extensions.ValidateBatch(ctx, target, opts.Extensions, false); err != nil {
			return nil, err
		}
	}

	price, err := s.lookupPrice(ctx, variantID, c.Currency)
	if err != nil {
		return nil, err
	}

	if err := s.invokeCartItemHook(ctx, hooks.HookCartAddItemBefore, cartID, customerID, variantID, quantity, c); err != nil {
		return nil, err
	}

	if err := c.AddItem(variantID, quantity, price); err != nil {
		return nil, apperror.Wrap(apperror.CodeValidation, "cannot add item", err)
	}
	stampLineAssignment(c, variantID, catalog.AssignmentContextFrom(ctx))

	if err := s.recalculate(ctx, c); err != nil {
		return nil, err
	}

	if err := s.enforceCartValidation(ctx, cartID, c); err != nil {
		return nil, err
	}

	if len(opts.Extensions) > 0 {
		target := domainext.CartItemTarget(cartID, variantID)
		if _, err := s.extensions.UpsertBatch(ctx, target, opts.Extensions, cartExtensionUpdatedBy(customerID, opts.UpdatedBy), false); err != nil {
			return nil, err
		}
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
	if err := s.invokeCartItemHook(ctx, hooks.HookCartAddItemAfter, c.ID, customerID, variantID, quantity, c); err != nil {
		s.log.Error("cart.add_item.after_hook_failed", err, map[string]interface{}{
			"cart_id":    c.ID,
			"variant_id": variantID,
		})
	}
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

	if err := s.invokeCartItemHook(ctx, hooks.HookCartUpdateItemBefore, cartID, customerID, variantID, quantity, c); err != nil {
		return nil, err
	}

	currentQty := 0
	for _, item := range c.Items {
		if item.VariantID == variantID {
			currentQty = item.Quantity
			break
		}
	}
	if quantity > currentQty {
		if err := s.ensureVariantPurchasable(ctx, variantID, c.Currency, quantity, lineAssignmentFromCart(c, variantID)); err != nil {
			return nil, err
		}
	}

	if err := c.UpdateItemQuantity(variantID, quantity); err != nil {
		return nil, apperror.Wrap(apperror.CodeValidation, "cannot update item", err)
	}

	if err := s.recalculate(ctx, c); err != nil {
		return nil, err
	}

	if err := s.enforceCartValidation(ctx, cartID, c); err != nil {
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

	if s.extensions != nil {
		target := domainext.CartItemTarget(cartID, variantID)
		if err := s.extensions.DeleteAllForTarget(ctx, target); err != nil {
			return nil, fmt.Errorf("cart service: remove item: delete extensions: %w", err)
		}
	}

	if err := s.recalculate(ctx, c); err != nil {
		return nil, err
	}

	if err := s.enforceCartValidation(ctx, cartID, c); err != nil {
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
	if err := s.invokeCartItemHook(ctx, hooks.HookCartRemoveItemAfter, c.ID, customerID, variantID, 0, c); err != nil {
		s.log.Error("cart.remove_item.after_hook_failed", err, map[string]interface{}{
			"cart_id":    c.ID,
			"variant_id": variantID,
		})
	}
	return c, nil
}

// recalculate runs the pricing pipeline over the cart's items and writes
// the computed prices back onto the cart.
func (s *Service) recalculate(ctx context.Context, c *domainCart.Cart) error {
	ctx = appPricing.WithUnitPriceCache(ctx)
	if len(c.Items) == 0 {
		return nil
	}

	pricingMeta := make(map[string]interface{})
	hookCtx := hooks.NewContext(hooks.HookCartRecalculateBefore)
	hookCtx.Set("cart_id", c.ID)
	hookCtx.Set("customer_id", c.CustomerID)
	hookCtx.Set("cart", c)
	hookCtx.Set("pricing_meta", pricingMeta)
	if err := s.invokeCartHook(ctx, hooks.HookCartRecalculateBefore, hookCtx); err != nil {
		return err
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
	if c.CustomerID != "" {
		pctx.Meta["customer_id"] = c.CustomerID
	}
	for k, v := range pricingMeta {
		pctx.Meta[k] = v
	}

	// Propagate store scope and storefront tax defaults when the request has
	// not provided explicit tax metadata yet.
	if st := store.FromContext(ctx); st != nil {
		pctx.Meta["store_id"] = st.ID
		if st.Country != "" {
			if _, ok := pctx.Meta["tax_country"]; !ok {
				pctx.Meta["tax_country"] = st.Country
			}
		}
		if _, ok := pctx.Meta["tax_country"]; ok {
			if _, ok := pctx.Meta["tax_mode"]; !ok {
				pctx.Meta["tax_mode"] = string(tax.ModeExclusive)
			}
		}
	}

	// Existing lines are repriced even after the product is archived. Add and
	// quantity increases still use the active-only scope in ensureVariantPurchasable.
	// Each line's resolved assignment is stored by variant so a dynamic bundle
	// leaf check cannot reuse a sibling line's parent.
	priceCtx := catalog.WithIncludeNonActiveProducts(ctx)
	priceCtx = catalog.WithPurchaseAssignments(priceCtx, purchaseAssignments(ctx, c))
	if err := s.pipeline.Execute(priceCtx, &pctx); err != nil {
		if customer, ok := s.bundlePriceCustomerError(priceCtx, err); ok {
			return customer
		}
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

func resultingLineQty(c *domainCart.Cart, variantID string, add int) (int, error) {
	if add < 1 {
		return 0, apperror.Validation("quantity must be positive")
	}
	current := 0
	if c != nil {
		for _, item := range c.Items {
			if item.VariantID == variantID {
				current = item.Quantity
				break
			}
		}
	}
	if current > math.MaxInt-add {
		return 0, apperror.Validation("requested quantity exceeds available stock")
	}
	return current + add, nil
}

// guestLineQtyForClaim is the quantity ensureVariantPurchasable must see.
// Assign (into == nil) checks the guest line alone. Merge checks the sum
// with the customer's existing line so a claim cannot raise that line
// above the available quantity.
func guestLineQtyForClaim(into *domainCart.Cart, variantID string, guestQty int) (int, error) {
	if into == nil {
		if guestQty < 1 {
			return 0, apperror.Validation("quantity must be positive")
		}
		return guestQty, nil
	}
	return resultingLineQty(into, variantID, guestQty)
}

const conflictingLinkedParentAssignmentMessage = "cart item already has a different linked parent assignment"

func assignmentsConflict(existing, incoming catalog.AssignmentContext) bool {
	if existing.Empty() || incoming.Empty() {
		return false
	}
	existing.ParentProductID = strings.TrimSpace(existing.ParentProductID)
	incoming.ParentProductID = strings.TrimSpace(incoming.ParentProductID)
	return existing.Deferred != incoming.Deferred || existing.ParentProductID != incoming.ParentProductID
}

func purchaseAssignments(ctx context.Context, c *domainCart.Cart) map[string]catalog.AssignmentContext {
	if c == nil {
		return map[string]catalog.AssignmentContext{}
	}
	out := make(map[string]catalog.AssignmentContext, len(c.Items))
	for _, item := range c.Items {
		out[item.VariantID] = catalog.ResolvePurchaseAssignment(ctx, lineAssignment(item))
	}
	return out
}

func lineAssignment(item domainCart.Item) catalog.AssignmentContext {
	return catalog.AssignmentContext{
		ParentProductID: item.AssignmentParentProductID,
		Deferred:        item.AssignmentDeferred,
	}
}

func lineAssignmentFromCart(c *domainCart.Cart, variantID string) catalog.AssignmentContext {
	if c == nil {
		return catalog.AssignmentContext{}
	}
	for _, item := range c.Items {
		if item.VariantID == variantID {
			return lineAssignment(item)
		}
	}
	return catalog.AssignmentContext{}
}

func stampLineAssignment(c *domainCart.Cart, variantID string, ac catalog.AssignmentContext) {
	if c == nil || ac.Empty() {
		return
	}
	for i := range c.Items {
		if c.Items[i].VariantID != variantID {
			continue
		}
		if lineAssignment(c.Items[i]).Empty() {
			c.Items[i].AssignmentParentProductID = ac.ParentProductID
			c.Items[i].AssignmentDeferred = ac.Deferred
		}
		return
	}
}

func (s *Service) ensureVariantPurchasable(ctx context.Context, variantID, currency string, lineQty int, line catalog.AssignmentContext) error {
	v, err := s.variants.FindByID(ctx, variantID)
	if err != nil {
		return fmt.Errorf("cart service: purchasability: lookup variant: %w", err)
	}
	if v == nil {
		return apperror.NotFound("variant not found")
	}
	p, err := s.products.FindByID(ctx, v.ProductID)
	if err != nil {
		return fmt.Errorf("cart service: purchasability: lookup product: %w", err)
	}
	if !catalog.ActiveForPurchase(p) {
		return apperror.Validation("product is not available for purchase")
	}
	if p.Type == catalog.TypeGrouped {
		return apperror.Validation(catalog.GroupedNotPurchasableMessage)
	}
	// Independent of visibility enforce, a nil facts source, and
	// BypassProductVisibility. HookCartAddItemBefore runs later and checkout
	// never calls it, so the gate has to live here (PR-1068). Line assignment
	// wins over the request context so claim and qty increase keep a stamp.
	// lineCtx carries that resolved assignment into visibility, price, and
	// leaf walks. It is not reused for another line.
	assignment := catalog.ResolvePurchaseAssignment(ctx, line)
	lineCtx := catalog.WithAssignmentContext(ctx, assignment)
	if catalog.LinkedParentBlocksPurchase(p, assignment) {
		return apperror.Validation(catalog.RequiresLinkedParentNotPurchasableMessage)
	}
	if p.Type == catalog.TypeBundle {
		if err := catalogapp.RejectRequiredLinkedParentLeaves(lineCtx, s.products, s.unitPrices.Components, p.ID, assignment); err != nil {
			return err
		}
	}
	// Listing HasPrice is a global-row flag. A dynamic bundle can be sellable
	// from a store-scoped component price that flag never sees. Other purchasable
	// axes still apply; the price gate is ResolveUnitPrice in the cart currency
	// and store.
	if p.Type == catalog.TypeBundle && p.PricingMode == catalog.PricingModeDynamic {
		if err := s.allowDynamicBundlePurchase(lineCtx, p, assignment); err != nil {
			return err
		}
		if err := s.requireDynamicBundlePrice(lineCtx, variantID, currency); err != nil {
			return err
		}
		return appinventory.RejectQtyAboveAvailable(lineCtx, s.stockSources(), p.ID, variantID, lineQty)
	}
	ok, err := catalog.AllowWithFacts(lineCtx, p, catalog.VisibilitySurfacePurchasable, s.visibilityFacts, s.visibilityOpts)
	if err != nil {
		return fmt.Errorf("cart service: purchasability: visibility: %w", err)
	}
	if !ok {
		return apperror.Validation("product is not currently purchasable")
	}
	return appinventory.RejectQtyAboveAvailable(lineCtx, s.stockSources(), p.ID, variantID, lineQty)
}

// allowDynamicBundlePurchase applies the purchasable axis without the listing
// HasPrice bit. Status, stock, store assignment, category, and a forced hidden
// mode still deny. A forced visible mode still allows the axis; ResolveUnitPrice
// remains the price gate.
func (s *Service) allowDynamicBundlePurchase(ctx context.Context, p *catalog.Product, assignment catalog.AssignmentContext) error {
	if catalog.BypassProductVisibility(ctx) || s.visibilityFacts == nil {
		return nil
	}
	m, err := s.visibilityFacts.LoadMany(ctx, []string{p.ID})
	if err != nil {
		return fmt.Errorf("cart service: purchasability: visibility: %w", err)
	}
	snap, ok := m[p.ID]
	if !ok {
		return apperror.Validation("product is not currently purchasable")
	}
	inputs := snap.Inputs
	inputs.HasPrice = true
	inputs.RequiresLinkedParent = snap.RequiresLinkedParent
	inputs.Assignment = assignment
	if !snap.Modes.Resolve(snap.Status, inputs, s.visibilityOpts).Purchasable {
		return apperror.Validation("product is not currently purchasable")
	}
	return nil
}

func (s *Service) requireDynamicBundlePrice(ctx context.Context, variantID, currency string) error {
	var storeID string
	if st := store.FromContext(ctx); st != nil {
		storeID = st.ID
	}
	_, err := appPricing.ResolveUnitPrice(ctx, s.priceSources(), variantID, currency, storeID)
	if err == nil {
		return nil
	}
	if appPricing.IsMissingUnitPrice(err) || apperror.Is(err, apperror.CodeValidation) || apperror.Is(err, apperror.CodeNotFound) {
		return catalogapp.BundleCustomerError(err)
	}
	return fmt.Errorf("cart service: purchasability: dynamic bundle price: %w", err)
}

// lookupPrice fetches the unit price for a variant in the cart's currency,
// scoped to the current store when available. It uses the same resolver as
// BasePriceStep so a dynamic bundle sum cannot disagree with recalculate.
func (s *Service) lookupPrice(ctx context.Context, variantID, currency string) (shared.Money, error) {
	var storeID string
	if st := store.FromContext(ctx); st != nil {
		storeID = st.ID
	}
	amount, err := appPricing.ResolveUnitPrice(ctx, s.priceSources(), variantID, currency, storeID)
	if err != nil {
		if customer, ok := s.bundlePriceCustomerError(ctx, err); ok {
			return shared.Money{}, customer
		}
		var appErr *apperror.Error
		if errors.As(err, &appErr) {
			return shared.Money{}, err
		}
		return shared.Money{}, fmt.Errorf("cart service: lookup price: %w", err)
	}
	return amount, nil
}

// bundlePriceCustomerError maps a bundle missing unit price, or a failure
// already wrapped as catalog.BundleCannotBeReservedMessage, onto that
// customer message. A missing price for any other product type uses
// appPricing.PriceNotAvailableMessage and does not name the variant.
// Component and bundle ids stay on Unwrap. Other failures return ok false.
func (s *Service) bundlePriceCustomerError(ctx context.Context, err error) (error, bool) {
	if err == nil {
		return nil, false
	}
	if appPricing.IsMissingUnitPrice(err) {
		isBundle, lookupErr := s.missingUnitPriceIsBundle(ctx, err)
		if lookupErr != nil {
			return lookupErr, true
		}
		if isBundle {
			return catalogapp.BundleCustomerError(err), true
		}
		return apperror.Wrap(apperror.CodeValidation, appPricing.PriceNotAvailableMessage, err), true
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		return nil, false
	}
	if appErr.Message == catalog.BundleCannotBeReservedMessage {
		return err, true
	}
	return nil, false
}

// missingUnitPriceIsBundle is true when the missing row belongs to a bundle.
// ResolveUnitPrice records the type it already loaded. A legacy stored lookup
// does not, so this falls back to one variant and one product read. A failed
// read is returned so the caller does not treat it as a non-bundle missing price.
func (s *Service) missingUnitPriceIsBundle(ctx context.Context, err error) (bool, error) {
	var missing *appPricing.MissingUnitPriceError
	if !errors.As(err, &missing) || missing == nil {
		return false, nil
	}
	if typ, ok := missing.KnownProductType(); ok {
		return typ == catalog.TypeBundle, nil
	}
	if s.variants == nil || s.products == nil || missing.VariantID == "" {
		return false, nil
	}
	v, lookupErr := s.variants.FindByID(ctx, missing.VariantID)
	if lookupErr != nil {
		return false, lookupErr
	}
	if v == nil {
		return false, nil
	}
	p, lookupErr := s.products.FindByID(ctx, v.ProductID)
	if lookupErr != nil {
		return false, lookupErr
	}
	if p == nil {
		return false, nil
	}
	return p.Type == catalog.TypeBundle, nil
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

	if err := s.enforceCartValidation(ctx, cartID, c); err != nil {
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

	if err := s.enforceCartValidation(ctx, cartID, c); err != nil {
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

func cartExtensionUpdatedBy(customerID, explicit string) string {
	if v := strings.TrimSpace(explicit); v != "" {
		return v
	}
	if v := strings.TrimSpace(customerID); v != "" {
		return v
	}
	return "guest"
}

func (s *Service) copyCartItemExtensions(ctx context.Context, fromCartID, toCartID, variantID string) error {
	if s.extensions == nil {
		return nil
	}
	from := domainext.CartItemTarget(fromCartID, variantID)
	to := domainext.CartItemTarget(toCartID, variantID)
	if err := s.extensions.CopyTarget(ctx, from, to, "system"); err != nil {
		return fmt.Errorf("cart service: copy extensions: %w", err)
	}
	return nil
}
