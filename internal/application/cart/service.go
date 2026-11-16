package cart

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	assignmentapp "github.com/akarso/shopanda/internal/application/assignment"
	catalogapp "github.com/akarso/shopanda/internal/application/catalog"
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
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

// mergeRestoreAttempts is how many times ReplaceTarget is tried before a
// claim returns with the pre-merge snapshot still held for the next claim.
const mergeRestoreAttempts = 3

// Service orchestrates cart use cases.
type Service struct {
	carts             domainCart.CartRepository
	prices            pricing.PriceRepository
	promotions        promotion.PromotionRepository
	coupons           promotion.CouponRepository
	variants          catalog.VariantRepository
	products          catalog.ProductRepository
	stock             inventory.StockRepository
	unitPrices        appPricing.UnitPriceSources
	visibilityFacts   catalog.VisibilityFactsSource
	visibilityOpts    catalog.VisibilityOptions
	downloadableFiles catalogapp.DownloadableFileCounter
	assignments       *assignmentapp.Flow
	pipeline          pricing.Pipeline
	extensions        extensionValueWriter
	hooks             *hooks.Registry
	log               logger.Logger
	bus               *event.Bus
}

type extensionValueWriter interface {
	ValidateBatch(ctx context.Context, target domainext.Target, inputs []domainext.ValueInput, canAccessPrivate bool) error
	UpsertBatch(ctx context.Context, target domainext.Target, inputs []domainext.ValueInput, updatedBy string, canAccessPrivate bool) ([]domainext.Value, error)
	UpsertBatchSelective(ctx context.Context, target domainext.Target, inputs []domainext.ValueInput, updatedBy string, canAccessPrivate bool, exclusiveFieldCodes map[string]struct{}) ([]domainext.Value, error)
	Delete(ctx context.Context, target domainext.Target, fieldCode string, canAccessPrivate bool) error
	DeleteAllForTarget(ctx context.Context, target domainext.Target) error
	DeleteOrphanCartItemExtensions(ctx context.Context, cartID string, keepVariantIDs []string) error
	CopyTarget(ctx context.Context, from, to domainext.Target, updatedBy string) error
	// ListByTarget / ReplaceTarget / ListTargetIDsByTypeAndIDPrefix back
	// merge compensation: CopyTarget commits independently of carts.Save,
	// so a Save failure must restore pre-merge rows. Backups use
	// TargetCartItemMergeBackup so a failed restore survives process restart.
	ListByTarget(ctx context.Context, target domainext.Target) ([]domainext.Value, error)
	ReplaceTarget(ctx context.Context, target domainext.Target, values []domainext.Value) error
	ListTargetIDsByTypeAndIDPrefix(ctx context.Context, targetType domainext.TargetType, idPrefix string) ([]string, error)
	// ListByCartItems is the plugin-facing read (private filtered unless includePrivate).
	ListByCartItems(ctx context.Context, cartID string, variantIDs []string, includePrivate bool) (map[string][]domainext.Value, error)
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

// WithAssignmentFlow checks IsEligible and parent-unit ownership when a
// parent is present (PR-1070). Nil skips those checks (tests).
func (s *Service) WithAssignmentFlow(flow *assignmentapp.Flow) *Service {
	if s == nil {
		return nil
	}
	s.assignments = flow
	return s
}

// WithDownloadableFiles wires the attachment count used to reject purchase of
// a downloadable product with zero files. Independent of visibility enforce,
// bypass, and a nil facts source (mirrors LinkedParentBlocksPurchase / PR-1068).
// Nil fails closed for TypeDownloadable lines and required downloadable bundle leaves.
func (s *Service) WithDownloadableFiles(files catalogapp.DownloadableFileCounter) *Service {
	if s == nil {
		return nil
	}
	s.downloadableFiles = files
	return s
}

func (s *Service) ensureDownloadableHasFile(ctx context.Context, p *catalog.Product) error {
	if p == nil || p.Type != catalog.TypeDownloadable {
		return nil
	}
	if s.downloadableFiles == nil {
		return apperror.Internal("cart: downloadable file repository is not configured")
	}
	n, err := s.downloadableFiles.CountByProductID(ctx, p.ID)
	if err != nil {
		return fmt.Errorf("cart service: count downloadable files: %w", err)
	}
	if catalog.DownloadableMissingFileBlocksPurchase(p, n) {
		return apperror.Validation(catalog.DownloadableMissingFileNotPurchasableMessage)
	}
	return nil
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

// EnsureActiveCart returns the customer's active cart or creates one.
// customerID must be non-empty. FindActiveByCustomerID only returns active
// carts, so a non-active leftover does not block creating a fresh cart.
func (s *Service) EnsureActiveCart(ctx context.Context, customerID, currency string) (*domainCart.Cart, error) {
	customerID = strings.TrimSpace(customerID)
	if customerID == "" {
		return nil, apperror.Validation("customer id must not be empty")
	}
	c, err := s.carts.FindActiveByCustomerID(ctx, customerID)
	if err != nil {
		return nil, fmt.Errorf("cart service: ensure active: %w", err)
	}
	if c != nil {
		return c, nil
	}
	return s.CreateCart(ctx, customerID, currency)
}

// DeleteLineExtension removes one stored extension value on a cart line.
// Verifies cart ownership first; PluginOnlyWrite still requires WithPluginWrite.
func (s *Service) DeleteLineExtension(ctx context.Context, cartID, customerID, variantID, fieldCode string) error {
	if s.extensions == nil {
		return apperror.Validation("extension values are not supported")
	}
	if _, err := s.GetCart(ctx, cartID, customerID); err != nil {
		return err
	}
	variantID = strings.TrimSpace(variantID)
	fieldCode = strings.TrimSpace(fieldCode)
	if variantID == "" || fieldCode == "" {
		return apperror.Validation("variant id and field code must not be empty")
	}
	if err := s.extensions.Delete(ctx, domainext.CartItemTarget(cartID, variantID), fieldCode, false); err != nil {
		return err
	}
	return nil
}

// UpsertLineExtension writes extension values on an existing cart line without
// changing quantity. ExclusiveString fields return Conflict when a stored
// non-empty string already differs; other strings may be rewritten.
func (s *Service) UpsertLineExtension(ctx context.Context, cartID, customerID, variantID string, inputs []domainext.ValueInput, updatedBy string) error {
	if s.extensions == nil {
		return apperror.Validation("extension values are not supported")
	}
	writeCtx := ctx
	ctx = extensionapp.StripPluginWrite(ctx)
	c, err := s.GetCart(ctx, cartID, customerID)
	if err != nil {
		return err
	}
	if !c.IsActive() {
		return apperror.Validation("cart: cannot modify non-active cart")
	}
	variantID = strings.TrimSpace(variantID)
	if variantID == "" || len(inputs) == 0 {
		return apperror.Validation("variant id and extension inputs must not be empty")
	}
	found := false
	for _, it := range c.Items {
		if it.VariantID == variantID {
			found = true
			break
		}
	}
	if !found {
		return apperror.NotFound("cart line not found")
	}
	target := domainext.CartItemTarget(cartID, variantID)
	if err := s.extensions.ValidateBatch(writeCtx, target, inputs, false); err != nil {
		return mapExtensionWriteErr(err)
	}
	if err := s.rejectExclusiveStringConflicts(ctx, cartID, variantID, inputs); err != nil {
		return err
	}
	return s.writeLineExtensions(writeCtx, target, inputs, cartExtensionUpdatedBy(customerID, updatedBy))
}

// rejectExclusiveStringConflicts returns Conflict when an ExclusiveString field
// already has a different non-empty value (fail-fast before cart Save).
func (s *Service) rejectExclusiveStringConflicts(ctx context.Context, cartID, variantID string, inputs []domainext.ValueInput) error {
	if s.extensions == nil || len(inputs) == 0 {
		return nil
	}
	var exclusive exclusiveStringChecker
	if e, ok := s.extensions.(exclusiveStringChecker); ok {
		exclusive = e
	}
	existing, err := s.extensions.ListByCartItems(ctx, cartID, []string{variantID}, false)
	if err != nil {
		return fmt.Errorf("cart service: list line extensions: %w", err)
	}
	byCode := map[string]domainext.Value{}
	for _, v := range existing[variantID] {
		byCode[v.FieldCode] = v
	}
	for _, in := range inputs {
		if exclusive == nil || !exclusive.IsExclusiveString(in.FieldCode) {
			continue
		}
		prev, ok := byCode[in.FieldCode]
		if !ok || prev.Payload.StringValue == nil {
			continue
		}
		stored := strings.TrimSpace(*prev.Payload.StringValue)
		want, _ := in.Value.(string)
		want = strings.TrimSpace(want)
		if stored != "" && stored != want {
			return apperror.Conflict(fmt.Sprintf("extension field %q already set to a different value", in.FieldCode))
		}
	}
	return nil
}

// writeLineExtensions upserts plain and ExclusiveString fields in one
// repository transaction so an exclusive conflict rolls back gift_note writes too.
func (s *Service) writeLineExtensions(ctx context.Context, target domainext.Target, inputs []domainext.ValueInput, updatedBy string) error {
	var exclusive exclusiveStringChecker
	if e, ok := s.extensions.(exclusiveStringChecker); ok {
		exclusive = e
	}
	exclCodes := map[string]struct{}{}
	for _, in := range inputs {
		if exclusive != nil && exclusive.IsExclusiveString(in.FieldCode) {
			exclCodes[in.FieldCode] = struct{}{}
		}
	}
	if _, err := s.extensions.UpsertBatchSelective(ctx, target, inputs, updatedBy, false, exclCodes); err != nil {
		return mapExtensionWriteErr(err)
	}
	return nil
}

func mapExtensionWriteErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, domainext.ErrStringValueConflict) {
		return apperror.Conflict("extension field already set to a different value")
	}
	// Leave domain ValidationError / unknown-field / plugin-only errors as-is;
	// HTTP adapters call extension.MapValueError.
	return err
}

// ExtensionFieldOwner returns the registrant that owns code, or "".
func (s *Service) ExtensionFieldOwner(code string) string {
	if po, ok := s.extensions.(pluginOnlyFieldChecker); ok {
		return po.FieldOwner(code)
	}
	return ""
}

// ExtensionFieldPluginOnly reports whether code is PluginOnlyWrite.
func (s *Service) ExtensionFieldPluginOnly(code string) bool {
	if po, ok := s.extensions.(pluginOnlyFieldChecker); ok {
		return po.IsPluginOnlyWrite(code)
	}
	return false
}

// ExtensionFieldExclusiveString reports whether code opts into exclusive string upsert.
func (s *Service) ExtensionFieldExclusiveString(code string) bool {
	if e, ok := s.extensions.(exclusiveStringChecker); ok {
		return e.IsExclusiveString(code)
	}
	return false
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
		// MergedGuestID is durable only after CopyTarget finished for every
		// claimable line; heal must not re-copy (see finishMergedGuestExtensionCopies).
		if err := s.finishMergedGuestExtensionCopies(ctx, guestCart, customerCart); err != nil {
			return nil, err
		}
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
func (s *Service) unclaimableGuestLines(ctx context.Context, customerID string, guestCart, into *domainCart.Cart) (map[string]struct{}, error) {
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
		err := s.ensureVariantPurchasable(ctx, customerID, item.VariantID, guestCart.Currency, lineQty, lineAssignment(item))
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
	dropped, err := s.unclaimableGuestLines(ctx, customerID, guestCart, nil)
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
	// Apply any durable pre-merge backup left when a prior claim's CopyTarget
	// committed but ReplaceTarget restore failed (possibly on another
	// process). Must run before a new snapshot — otherwise the retry would
	// capture the guest overwrite as the "pre-merge" state.
	if err := s.applyPendingMergeRestore(ctx, customerCart.ID, guestCart.ID); err != nil {
		return nil, err
	}

	// Validate every guest line before mutating. CopyTarget runs before
	// MergedGuestID is persisted so a failed copy never looks like a finished
	// merge on retry (which would delete guest rows via finishMergedGuestCleanup).
	// Guest extension deletes wait until after a successful Save with MergedGuestID.
	dropped, err := s.unclaimableGuestLines(ctx, customerCart.CustomerID, guestCart, customerCart)
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
	}
	var copiedVariants []string
	for _, item := range guestCart.Items {
		if _, skip := dropped[item.VariantID]; skip {
			continue
		}
		if err := customerCart.AddItem(item.VariantID, item.Quantity, item.UnitPrice); err != nil {
			return nil, apperror.Wrap(apperror.CodeValidation, "cannot merge guest cart item", err)
		}
		stampLineAssignment(customerCart, item.VariantID, lineAssignment(item))
		copiedVariants = append(copiedVariants, item.VariantID)
	}
	if err := s.applyEligibleGuestCoupon(ctx, customerCart, guestCart.CouponCode); err != nil {
		return nil, err
	}
	if err := s.recalculate(ctx, customerCart); err != nil {
		return nil, err
	}

	// Snapshot + durable backup before any CopyTarget. CopyTarget commits
	// independently of carts.Save; backups live in extension_values so a
	// failed restore (or process death) can still recover the customer value.
	snapshots, err := s.snapshotCustomerCartExtensions(ctx, customerCart.ID, copiedVariants)
	if err != nil {
		return nil, err
	}
	if err := s.writeMergeBackups(ctx, customerCart.ID, guestCart.ID, snapshots); err != nil {
		return nil, err
	}

	// Copy while guest rows still exist and before MergedGuestID is durable.
	// A failure partway through must restore only variants CopyTarget already
	// changed. A Save conflict after another claim already committed this
	// merge must NOT restore (that would wipe the winner's guest values).
	var copiedOK []string
	for _, variantID := range copiedVariants {
		if err := s.copyCartItemExtensions(ctx, guestCart.ID, customerCart.ID, variantID); err != nil {
			toRestore := snapshotsForVariants(snapshots, copiedOK)
			if restoreErr := s.restoreCustomerCartExtensions(ctx, customerCart.ID, guestCart.ID, toRestore); restoreErr != nil {
				return nil, fmt.Errorf("cart service: claim guest cart: copy extensions failed (%v); restore customer extensions: %w", err, restoreErr)
			}
			return nil, err
		}
		copiedOK = append(copiedOK, variantID)
	}
	customerCart.MergedGuestID = guestCart.ID
	if err := s.carts.Save(ctx, customerCart); err != nil {
		// Concurrent claim may have already committed this merge.
		if latest, findErr := s.carts.FindByID(ctx, customerCart.ID); findErr == nil && latest != nil && latest.MergedGuestID == guestCart.ID {
			_ = s.clearMergeBackups(ctx, customerCart.ID, guestCart.ID)
			if cleanupErr := s.finishMergedGuestCleanup(ctx, guestCart.ID); cleanupErr != nil {
				return nil, cleanupErr
			}
			return latest, nil
		}
		toRestore := snapshotsForVariants(snapshots, copiedOK)
		if restoreErr := s.restoreCustomerCartExtensions(ctx, customerCart.ID, guestCart.ID, toRestore); restoreErr != nil {
			return nil, fmt.Errorf("cart service: claim guest cart: save merged cart failed (%v); restore customer extensions: %w", err, restoreErr)
		}
		return nil, fmt.Errorf("cart service: claim guest cart: save merged cart: %w", err)
	}
	// Merge is durable — guest overwrite is intentional; drop backups.
	_ = s.clearMergeBackups(ctx, customerCart.ID, guestCart.ID)
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

// finishMergedGuestExtensionCopies used to re-copy every guest variant that
// also existed on the customer cart. That is unsafe: unclaimableGuestLines
// drops a guest line without removing it from the guest cart, and a
// pre-existing customer line for the same variant stays in place — so a heal
// after durable MergedGuestID would overwrite that customer line's
// extensions with the dropped guest's values.
//
// CopyTarget now always runs to completion before MergedGuestID is saved, so
// a durable MergedGuestID already means every claimable guest line was
// copied. Heal therefore must not copy again; it only exists so the claim
// retry path still has a named step before finishMergedGuestCleanup.
func (s *Service) finishMergedGuestExtensionCopies(ctx context.Context, guestCart, customerCart *domainCart.Cart) error {
	_ = ctx
	_ = guestCart
	_ = customerCart
	return nil
}

// customerCartExtensionSnapshot is a point-in-time copy of one target's
// stored extension rows, taken before a destructive write that is not
// atomic with the cart Save that follows it.
type customerCartExtensionSnapshot struct {
	target domainext.Target
	values []domainext.Value
}

// snapshotCustomerCartExtensions reads the customer cart's current extension
// rows for each variant in variantIDs, before any CopyTarget touches them.
func (s *Service) snapshotCustomerCartExtensions(ctx context.Context, customerCartID string, variantIDs []string) ([]customerCartExtensionSnapshot, error) {
	if s.extensions == nil || len(variantIDs) == 0 {
		return nil, nil
	}
	out := make([]customerCartExtensionSnapshot, 0, len(variantIDs))
	for _, variantID := range variantIDs {
		target := domainext.CartItemTarget(customerCartID, variantID)
		values, err := s.extensions.ListByTarget(ctx, target)
		if err != nil {
			return nil, fmt.Errorf("cart service: claim guest cart: snapshot customer extensions: %w", err)
		}
		out = append(out, customerCartExtensionSnapshot{target: target, values: values})
	}
	return out, nil
}

// snapshotsForVariants returns the subset of snapshots whose cart-item
// target variant is in variantIDs (the variants CopyTarget already changed).
func snapshotsForVariants(snapshots []customerCartExtensionSnapshot, variantIDs []string) []customerCartExtensionSnapshot {
	if len(snapshots) == 0 || len(variantIDs) == 0 {
		return nil
	}
	want := make(map[string]struct{}, len(variantIDs))
	for _, id := range variantIDs {
		want[id] = struct{}{}
	}
	out := make([]customerCartExtensionSnapshot, 0, len(variantIDs))
	for _, snap := range snapshots {
		_, variantID, ok := domainext.ParseCartItemTargetID(snap.target.ID)
		if !ok {
			continue
		}
		if _, ok := want[variantID]; !ok {
			continue
		}
		out = append(out, snap)
	}
	return out
}

// writeMergeBackups persists pre-merge snapshots under TargetCartItemMergeBackup
// so a failed restore (or another API process) can still recover them.
func (s *Service) writeMergeBackups(ctx context.Context, customerCartID, guestCartID string, snapshots []customerCartExtensionSnapshot) error {
	if s.extensions == nil || len(snapshots) == 0 {
		return nil
	}
	for _, snap := range snapshots {
		_, variantID, ok := domainext.ParseCartItemTargetID(snap.target.ID)
		if !ok {
			continue
		}
		backup := domainext.MergeBackupTarget(customerCartID, guestCartID, variantID)
		vals := make([]domainext.Value, len(snap.values))
		for i, v := range snap.values {
			v.TargetType = backup.Type
			v.TargetID = backup.ID
			vals[i] = v
		}
		if err := s.extensions.ReplaceTarget(ctx, backup, vals); err != nil {
			return fmt.Errorf("cart service: claim guest cart: write merge backup for %q: %w", variantID, err)
		}
	}
	return nil
}

// clearMergeBackups deletes durable pre-merge backups for this guest merge.
func (s *Service) clearMergeBackups(ctx context.Context, customerCartID, guestCartID string) error {
	if s.extensions == nil || customerCartID == "" || guestCartID == "" {
		return nil
	}
	prefix := customerCartID + ":" + guestCartID + ":"
	ids, err := s.extensions.ListTargetIDsByTypeAndIDPrefix(ctx, domainext.TargetCartItemMergeBackup, prefix+"%")
	if err != nil {
		return fmt.Errorf("cart service: claim guest cart: list merge backups: %w", err)
	}
	for _, id := range ids {
		if err := s.extensions.DeleteAllForTarget(ctx, domainext.Target{Type: domainext.TargetCartItemMergeBackup, ID: id}); err != nil {
			return fmt.Errorf("cart service: claim guest cart: clear merge backup %q: %w", id, err)
		}
	}
	return nil
}

// loadMergeBackups reads durable pre-merge snapshots for this guest merge.
func (s *Service) loadMergeBackups(ctx context.Context, customerCartID, guestCartID string) ([]customerCartExtensionSnapshot, error) {
	if s.extensions == nil || customerCartID == "" || guestCartID == "" {
		return nil, nil
	}
	prefix := customerCartID + ":" + guestCartID + ":"
	ids, err := s.extensions.ListTargetIDsByTypeAndIDPrefix(ctx, domainext.TargetCartItemMergeBackup, prefix+"%")
	if err != nil {
		return nil, err
	}
	out := make([]customerCartExtensionSnapshot, 0, len(ids))
	for _, id := range ids {
		_, _, variantID, ok := domainext.ParseMergeBackupTargetID(id)
		if !ok {
			continue
		}
		backup := domainext.Target{Type: domainext.TargetCartItemMergeBackup, ID: id}
		values, err := s.extensions.ListByTarget(ctx, backup)
		if err != nil {
			return nil, err
		}
		// Remap to the live cart_item target for ReplaceTarget restore.
		live := domainext.CartItemTarget(customerCartID, variantID)
		remapped := make([]domainext.Value, len(values))
		for i, v := range values {
			v.TargetType = live.Type
			v.TargetID = live.ID
			remapped[i] = v
		}
		out = append(out, customerCartExtensionSnapshot{target: live, values: remapped})
	}
	return out, nil
}

// applyPendingMergeRestore restores durable pre-merge backups (if any) before
// a new claim may snapshot or CopyTarget.
func (s *Service) applyPendingMergeRestore(ctx context.Context, customerCartID, guestCartID string) error {
	snapshots, err := s.loadMergeBackups(ctx, customerCartID, guestCartID)
	if err != nil {
		return fmt.Errorf("cart service: claim guest cart: load merge backups: %w", err)
	}
	if len(snapshots) == 0 {
		return nil
	}
	return s.restoreCustomerCartExtensions(ctx, customerCartID, guestCartID, snapshots)
}

// restoreCustomerCartExtensions puts each snapshotted target back exactly as
// it was via ReplaceTarget, retrying each target before giving up. On success
// durable merge backups for this guest are cleared. On failure the backups
// remain so another process (or retry) can restore instead of snapshotting
// the guest overwrite. Callers must pass only snapshots for variants
// CopyTarget already changed (or the full backup set when applying a prior
// failed restore).
func (s *Service) restoreCustomerCartExtensions(ctx context.Context, customerCartID, guestCartID string, snapshots []customerCartExtensionSnapshot) error {
	if s.extensions == nil || len(snapshots) == 0 {
		_ = s.clearMergeBackups(ctx, customerCartID, guestCartID)
		return nil
	}
	for _, snap := range snapshots {
		var lastErr error
		for attempt := 0; attempt < mergeRestoreAttempts; attempt++ {
			lastErr = s.extensions.ReplaceTarget(ctx, snap.target, snap.values)
			if lastErr == nil {
				break
			}
		}
		if lastErr != nil {
			return fmt.Errorf("cart service: claim guest cart: restore customer extensions for %q: %w", snap.target.ID, lastErr)
		}
	}
	if err := s.clearMergeBackups(ctx, customerCartID, guestCartID); err != nil {
		return err
	}
	return nil
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
	// A plugin write marker is honoured only for the extension writes below; hooks,
	// pricing and event handlers receive ctx without it.
	writeCtx := ctx
	ctx = extensionapp.StripPluginWrite(ctx)
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
	if err := s.ensureVariantPurchasable(ctx, customerID, variantID, c.Currency, lineQty, lineAssignmentFromCart(c, variantID)); err != nil {
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
		if err := s.extensions.ValidateBatch(writeCtx, target, opts.Extensions, false); err != nil {
			return nil, mapExtensionWriteErr(err)
		}
		// Fail before Save so a known exclusive conflict never commits quantity first.
		if err := s.rejectExclusiveStringConflicts(ctx, cartID, variantID, opts.Extensions); err != nil {
			return nil, err
		}
	}

	price, err := s.lookupPrice(ctx, variantID, c.Currency)
	if err != nil {
		return nil, err
	}

	if err := s.invokeCartItemHook(ctx, hooks.HookCartAddItemBefore, cartID, customerID, variantID, quantity, c, cartItemHookOpts{
		extensions: opts.Extensions,
		writeCtx:   writeCtx,
	}); err != nil {
		return nil, err
	}

	// Snapshot every line before AddItem+recalculate: a failed extension
	// upsert must restore sibling unit prices too, not only the touched line.
	linesSnap := snapshotCartLines(c)

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

	// Persist cart first; extension upsert runs only after successful Save.
	// On upsert failure, compensate by restoring the prior cart-line snapshot.
	if err := s.carts.Save(ctx, c); err != nil {
		return nil, fmt.Errorf("cart service: add item: save: %w", err)
	}

	if len(opts.Extensions) > 0 {
		target := domainext.CartItemTarget(cartID, variantID)
		// ExclusiveString fields use conditional ON CONFLICT upsert; others use plain UpsertBatch.
		if err := s.writeLineExtensions(writeCtx, target, opts.Extensions, cartExtensionUpdatedBy(customerID, opts.UpdatedBy)); err != nil {
			if compErr := s.compensateAddItemCart(ctx, c, linesSnap); compErr != nil {
				return nil, fmt.Errorf("cart service: add item: extension upsert failed (%v); compensate: %w", err, compErr)
			}
			return nil, err
		}
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
	if err := s.invokeCartItemHook(ctx, hooks.HookCartAddItemAfter, c.ID, customerID, variantID, quantity, c, cartItemHookOpts{
		extensions: opts.Extensions,
		writeCtx:   writeCtx,
	}); err != nil {
		s.log.Error("cart.add_item.after_hook_failed", err, map[string]interface{}{
			"cart_id":    c.ID,
			"variant_id": variantID,
		})
	}
	return c, nil
}

type lineSnapshot struct {
	variantID string
	quantity  int
	unit      shared.Money
	assign    catalog.AssignmentContext
}

// snapshotCartLines captures every line's quantity, unit price, and
// assignment before AddItem+recalculate mutates the cart.
func snapshotCartLines(c *domainCart.Cart) []lineSnapshot {
	if c == nil || len(c.Items) == 0 {
		return nil
	}
	out := make([]lineSnapshot, 0, len(c.Items))
	for _, item := range c.Items {
		out = append(out, lineSnapshot{
			variantID: item.VariantID,
			quantity:  item.Quantity,
			unit:      item.UnitPrice,
			assign:    lineAssignment(item),
		})
	}
	return out
}

// compensateAddItemCart restores the cart to its pre-AddItem line shape
// (including sibling unit prices that recalculate may have rewritten) and
// re-saves. It deliberately does not call recalculate: compensation must
// undo the mutation, not reprice against the current catalog.
func (s *Service) compensateAddItemCart(ctx context.Context, c *domainCart.Cart, snap []lineSnapshot) error {
	keep := make(map[string]lineSnapshot, len(snap))
	for _, line := range snap {
		keep[line.variantID] = line
	}
	// Remove lines that were not present before AddItem (the newly added variant).
	for _, item := range append([]domainCart.Item(nil), c.Items...) {
		if _, ok := keep[item.VariantID]; ok {
			continue
		}
		if err := c.RemoveItem(item.VariantID); err != nil {
			return err
		}
	}
	for _, line := range snap {
		if err := c.UpdateItemQuantity(line.variantID, line.quantity); err != nil {
			return err
		}
		for i := range c.Items {
			if c.Items[i].VariantID != line.variantID {
				continue
			}
			c.Items[i].UnitPrice = line.unit
			c.Items[i].AssignmentParentProductID = line.assign.ParentProductID
			c.Items[i].AssignmentParentUnitID = line.assign.ParentUnitID
			c.Items[i].AssignmentDeferred = line.assign.Deferred
			break
		}
	}
	if err := s.carts.Save(ctx, c); err != nil {
		return fmt.Errorf("re-save after extension failure: %w", err)
	}
	return nil
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

	if err := s.invokeCartItemHook(ctx, hooks.HookCartUpdateItemBefore, cartID, customerID, variantID, quantity, c, cartItemHookOpts{}); err != nil {
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
		if err := s.ensureVariantPurchasable(ctx, customerID, variantID, c.Currency, quantity, lineAssignmentFromCart(c, variantID)); err != nil {
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
	if err := s.invokeCartItemHook(ctx, hooks.HookCartRemoveItemAfter, c.ID, customerID, variantID, 0, c, cartItemHookOpts{}); err != nil {
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
	hookCtx.Set("cart", cartViewFromDomain(c))
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
	if err := s.ensureTaxClasses(priceCtx, c, pctx.Meta); err != nil {
		return err
	}
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

// ensureTaxClasses fills Meta["tax_classes"] (variantID → class) from product
// type via shared appPricing.EnsureTaxClasses. Existing keys (hooks /
// explicit overrides) are left untouched; empty/unknown overrides error.
//
// Variants and products needed for fill-missing are loaded via
// catalog.LookupVariants / LookupProducts (FindByIDs / ListByIDs when the
// repository implements them). BasePriceStep still resolves unit prices
// through its own catalog reads.
func (s *Service) ensureTaxClasses(ctx context.Context, c *domainCart.Cart, meta map[string]interface{}) error {
	overrides := appPricing.TaxClassesFromMeta(meta)
	needVariant := make([]string, 0, len(c.Items))
	seenVariant := make(map[string]struct{}, len(c.Items))
	for _, item := range c.Items {
		if overrides != nil {
			if _, ok := overrides[item.VariantID]; ok {
				continue
			}
		}
		if _, ok := seenVariant[item.VariantID]; ok {
			continue
		}
		seenVariant[item.VariantID] = struct{}{}
		needVariant = append(needVariant, item.VariantID)
	}

	variants, err := catalog.LookupVariants(ctx, s.variants, needVariant)
	if err != nil {
		return fmt.Errorf("cart service: tax class: lookup variants: %w", err)
	}
	productIDs := make([]string, 0, len(variants))
	seenProduct := make(map[string]struct{}, len(variants))
	for _, v := range variants {
		if v == nil || v.ProductID == "" {
			continue
		}
		if _, ok := seenProduct[v.ProductID]; ok {
			continue
		}
		seenProduct[v.ProductID] = struct{}{}
		productIDs = append(productIDs, v.ProductID)
	}
	products, err := catalog.LookupProducts(ctx, s.products, productIDs)
	if err != nil {
		return fmt.Errorf("cart service: tax class: lookup products: %w", err)
	}

	lines := make([]appPricing.LineTaxClass, 0, len(c.Items))
	for _, item := range c.Items {
		if overrides != nil {
			if _, ok := overrides[item.VariantID]; ok {
				lines = append(lines, appPricing.LineTaxClass{VariantID: item.VariantID})
				continue
			}
		}
		v := variants[item.VariantID]
		if v == nil {
			// Skipped/invalid lookup id must not become TaxClassCode("") (standard).
			return apperror.Validation(fmt.Sprintf("variant %q not found or invalid", item.VariantID))
		}
		p := products[v.ProductID]
		if p == nil {
			return apperror.NotFound(fmt.Sprintf("product for variant %q not found", item.VariantID))
		}
		lines = append(lines, appPricing.LineTaxClass{VariantID: item.VariantID, ProductType: p.Type})
	}
	return appPricing.EnsureTaxClasses(meta, lines)
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
	existing.ParentUnitID = strings.TrimSpace(existing.ParentUnitID)
	incoming.ParentProductID = strings.TrimSpace(incoming.ParentProductID)
	incoming.ParentUnitID = strings.TrimSpace(incoming.ParentUnitID)
	return existing.Deferred != incoming.Deferred ||
		existing.ParentProductID != incoming.ParentProductID ||
		existing.ParentUnitID != incoming.ParentUnitID
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
		ParentUnitID:    item.AssignmentParentUnitID,
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
			c.Items[i].AssignmentParentUnitID = ac.ParentUnitID
			c.Items[i].AssignmentDeferred = ac.Deferred
		}
		return
	}
}

func (s *Service) checkAssignmentPurchase(ctx context.Context, customerID string, p *catalog.Product, assignment catalog.AssignmentContext) error {
	ids, err := catalogapp.LinkedParentChildProductIDs(ctx, s.products, s.unitPrices.Components, p)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		ids = []string{p.ID}
	}
	return s.assignments.CheckPurchaseAll(ctx, customerID, ids, assignment)
}

func (s *Service) ensureVariantPurchasable(ctx context.Context, customerID, variantID, currency string, lineQty int, line catalog.AssignmentContext) error {
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
	if err := s.ensureDownloadableHasFile(ctx, p); err != nil {
		return err
	}
	if s.assignments != nil && (strings.TrimSpace(assignment.ParentProductID) != "" || strings.TrimSpace(assignment.ParentUnitID) != "") {
		if err := s.checkAssignmentPurchase(ctx, customerID, p, assignment); err != nil {
			return err
		}
	}
	if p.Type == catalog.TypeBundle {
		if err := catalogapp.RejectRequiredLinkedParentLeaves(lineCtx, s.products, s.unitPrices.Components, p.ID, assignment); err != nil {
			return err
		}
		if err := catalogapp.RejectRequiredMissingDownloadableFileLeaves(lineCtx, s.products, s.unitPrices.Components, s.downloadableFiles, p.ID); err != nil {
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
