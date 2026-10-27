package assignment

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	catalogapp "github.com/akarso/shopanda/internal/application/catalog"
	domain "github.com/akarso/shopanda/internal/domain/assignment"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

const (
	// ParentNotEligibleMessage is the cart, checkout, and post-purchase
	// text when IsEligible is false for a named parent.
	ParentNotEligibleMessage = "linked parent is not eligible for this product"
	// ParentNotOwnedMessage is returned when the parent unit is missing
	// or belongs to someone else.
	ParentNotOwnedMessage = "linked parent instance is not owned by this customer"
	// ChildNotOwnedMessage is returned when the child unit is missing
	// or belongs to someone else.
	ChildNotOwnedMessage = "assignment child instance is not owned by this customer"
	// ParentUnitSignInMessage is returned when a guest names a parent unit.
	ParentUnitSignInMessage = "sign in to assign this product to an owned instance"
	// ParentUnitMismatchMessage is returned when the stamped parent
	// product does not match the parent unit's product.
	ParentUnitMismatchMessage = "linked parent product does not match the selected instance"
	// AlreadyAssignedMessage is returned when post-purchase assign
	// finds a live assigned row.
	AlreadyAssignedMessage = "child already has an active assignment"
)

// Eligibility is the catalog-rule predicate used when a parent is present.
type Eligibility interface {
	IsEligible(ctx context.Context, childID, parentID string) (bool, error)
}

// OrderLookup is the order reads Flow needs for ownership.
type OrderLookup interface {
	FindByID(ctx context.Context, id string) (*order.Order, error)
	FindByCustomerID(ctx context.Context, customerID string) ([]order.Order, error)
}

// VariantLookup is the variant read Flow needs to resolve a unit's product.
type VariantLookup interface {
	FindByID(ctx context.Context, id string) (*catalog.Variant, error)
}

// ProductLookup is the product read used to resolve required linked
// leaves for a non-exploded bundle (AssignOwned and ListOwnedParents).
type ProductLookup interface {
	FindByID(ctx context.Context, id string) (*catalog.Product, error)
}

// BundleComponentLookup lists required bundle leaves for eligibility.
type BundleComponentLookup interface {
	ListByBundleID(ctx context.Context, bundleID string) ([]catalog.BundleComponent, error)
}

// VariantBatchLookup is an optional batch read. postgres implements it
// so ListOwnedParents does not FindByID once per historical line.
type VariantBatchLookup interface {
	FindByIDs(ctx context.Context, ids []string) (map[string]*catalog.Variant, error)
}

// PaymentLookup is the payment read Flow needs to decide ownership.
// A unit is owned only after payment completed, or when no payment
// was required (payable 0). Pending and failed payments are not owned.
type PaymentLookup interface {
	FindByOrderID(ctx context.Context, orderID string) (*payment.Payment, error)
}

// PaymentBatchLookup is an optional batch read. postgres implements it
// so ListOwnedParents does not FindByOrderID once per historical order.
type PaymentBatchLookup interface {
	FindByOrderIDs(ctx context.Context, orderIDs []string) (map[string]*payment.Payment, error)
}

// PurchaseLine is one RequiresLinkedParent order line to persist.
type PurchaseLine struct {
	ChildVariantID string
	ChildProductID string
	// EligibleProductIDs are the catalog products IsEligible must accept.
	// Empty means ChildProductID. A non-exploded bundle uses the bundle
	// variant as the unit and the linked leaves here.
	EligibleProductIDs []string
	Assignment         catalog.AssignmentContext
}

// OwnedParent is an owned order line that IsEligible accepts for a child.
type OwnedParent struct {
	UnitID    string
	OrderID   string
	VariantID string
	ProductID string
	Name      string
}

type repositoryTxBinder interface {
	WithTx(*sql.Tx) domain.Repository
}

// Flow is the PR-1070 assignment path: eligibility and ownership when a
// parent is present, cart-time CreateAssigned / CreateUnassigned on
// purchase, and post-purchase Assign. State transitions stay on Service.
type Flow struct {
	assignments *Service
	repo        domain.Repository
	eligibility Eligibility
	orders      OrderLookup
	variants    VariantLookup
	payments    PaymentLookup
	products    ProductLookup
	components  BundleComponentLookup
}

// NewFlow creates the assignment flow. Every dependency is required so
// a parent cannot skip IsEligible or ownership.
func NewFlow(
	assignments *Service,
	repo domain.Repository,
	eligibility Eligibility,
	orders OrderLookup,
	variants VariantLookup,
	payments PaymentLookup,
) *Flow {
	if assignments == nil {
		panic("assignment.NewFlow: nil assignments service")
	}
	if repo == nil {
		panic("assignment.NewFlow: nil assignments repository")
	}
	if eligibility == nil {
		panic("assignment.NewFlow: nil eligibility")
	}
	if orders == nil {
		panic("assignment.NewFlow: nil orders")
	}
	if variants == nil {
		panic("assignment.NewFlow: nil variants")
	}
	if payments == nil {
		panic("assignment.NewFlow: nil payments")
	}
	return &Flow{
		assignments: assignments,
		repo:        repo,
		eligibility: eligibility,
		orders:      orders,
		variants:    variants,
		payments:    payments,
	}
}

// WithLinkedLeaves lets AssignOwned and ListOwnedParents resolve required
// linked leaves the same way checkout does. Optional; without it, a
// non-exploded bundle is checked as its own product id.
func (f *Flow) WithLinkedLeaves(products ProductLookup, components BundleComponentLookup) *Flow {
	if f == nil {
		return nil
	}
	f.products = products
	f.components = components
	return f
}

// WithTx returns a flow whose writes use the bound assignment repo.
// The domain port does not declare WithTx; callers type-assert the
// postgres adapter. When the repo cannot bind, the same flow is
// returned and writes stay on the base repo.
func (f *Flow) WithTx(tx *sql.Tx) *Flow {
	if f == nil || tx == nil {
		return f
	}
	binder, ok := f.repo.(repositoryTxBinder)
	if !ok {
		return f
	}
	bound := binder.WithTx(tx)
	cp := *f
	cp.repo = bound
	cp.assignments = NewService(bound)
	return &cp
}

// BindsTx reports whether WithTx will actually bind the assignment repo.
func (f *Flow) BindsTx() bool {
	if f == nil {
		return false
	}
	_, ok := f.repo.(repositoryTxBinder)
	return ok
}

// CheckPurchase validates a named parent. Deferred-only or an empty
// context is accepted (purchasability is PR-1068). A parent product or
// unit requires IsEligible. A unit also requires customer ownership.
func (f *Flow) CheckPurchase(ctx context.Context, customerID, childProductID string, ac catalog.AssignmentContext) error {
	if f == nil {
		return fmt.Errorf("assignment flow: not configured")
	}
	childProductID = strings.TrimSpace(childProductID)
	if childProductID == "" {
		return apperror.Validation("child product id must not be empty")
	}
	ac.ParentProductID = strings.TrimSpace(ac.ParentProductID)
	ac.ParentUnitID = strings.TrimSpace(ac.ParentUnitID)
	if ac.ParentProductID == "" && ac.ParentUnitID == "" {
		return nil
	}
	parentProductID, err := f.resolveParentProduct(ctx, strings.TrimSpace(customerID), ac)
	if err != nil {
		return err
	}
	ok, err := f.eligibility.IsEligible(ctx, childProductID, parentProductID)
	if err != nil {
		if apperror.Is(err, apperror.CodeValidation) {
			return err
		}
		return fmt.Errorf("assignment flow: eligibility: %w", err)
	}
	if !ok {
		return apperror.Validation(ParentNotEligibleMessage)
	}
	return nil
}

// RecordPurchase writes one ProductAssignment per RequiresLinkedParent
// line. A parent unit becomes CreateAssigned; deferred or product-only
// becomes CreateUnassigned. This is the only checkout write policy.
func (f *Flow) RecordPurchase(ctx context.Context, customerID, orderID string, lines []PurchaseLine) error {
	if f == nil {
		return fmt.Errorf("assignment flow: not configured")
	}
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return apperror.Validation("order id must not be empty")
	}
	customerID = strings.TrimSpace(customerID)
	for _, line := range lines {
		childVariantID := strings.TrimSpace(line.ChildVariantID)
		childUnit := domain.UnitID(orderID, childVariantID)
		if childUnit == "" {
			return apperror.Validation("child variant id must not be empty")
		}
		if err := f.CheckPurchaseAll(ctx, customerID, line.eligibilityIDs(), line.Assignment); err != nil {
			return err
		}
		parentUnit := strings.TrimSpace(line.Assignment.ParentUnitID)
		if parentUnit != "" {
			canonical, ok := domain.CanonicalUnitID(parentUnit)
			if !ok {
				return apperror.Validation(ParentNotOwnedMessage)
			}
			if _, err := f.assignments.CreateAssigned(ctx, canonical, childUnit); err != nil {
				return err
			}
			continue
		}
		if _, err := f.assignments.CreateUnassigned(ctx, childUnit); err != nil {
			return err
		}
	}
	return nil
}

func (line PurchaseLine) eligibilityIDs() []string {
	if len(line.EligibleProductIDs) > 0 {
		return line.EligibleProductIDs
	}
	if strings.TrimSpace(line.ChildProductID) == "" {
		return nil
	}
	return []string{line.ChildProductID}
}

// CheckPurchaseAll runs CheckPurchase for each child product id.
func (f *Flow) CheckPurchaseAll(ctx context.Context, customerID string, childProductIDs []string, ac catalog.AssignmentContext) error {
	seen := make(map[string]struct{}, len(childProductIDs))
	for _, id := range childProductIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if err := f.CheckPurchase(ctx, customerID, id, ac); err != nil {
			return err
		}
	}
	return nil
}

// RevokeForOrder revokes every current assignment whose child unit is
// an order line on orderID. Missing or already-revoked rows are skipped.
func (f *Flow) RevokeForOrder(ctx context.Context, orderID string) error {
	if f == nil {
		return fmt.Errorf("assignment flow: not configured")
	}
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return apperror.Validation("order id must not be empty")
	}
	o, err := f.orders.FindByID(ctx, orderID)
	if err != nil {
		return fmt.Errorf("assignment flow: lookup order: %w", err)
	}
	if o == nil {
		return nil
	}
	for _, item := range o.Items() {
		childUnit := domain.UnitID(orderID, item.VariantID)
		current, err := f.assignments.CurrentByChild(ctx, childUnit)
		if err != nil {
			return err
		}
		if current == nil {
			continue
		}
		if _, err := f.assignments.Revoke(ctx, current.ID); err != nil {
			return err
		}
	}
	return nil
}

// AssignOwned binds an unassigned child the customer owns to a parent
// unit they own. A missing current row inserts assigned directly so a
// pre-1070 purchase can still be bound. A live assigned row is a conflict.
func (f *Flow) AssignOwned(ctx context.Context, customerID, childUnitID, parentUnitID string) (*domain.ProductAssignment, error) {
	if f == nil {
		return nil, fmt.Errorf("assignment flow: not configured")
	}
	customerID = strings.TrimSpace(customerID)
	if customerID == "" {
		return nil, apperror.Validation(ParentUnitSignInMessage)
	}
	childUnit, ok := domain.CanonicalUnitID(childUnitID)
	if !ok {
		return nil, apperror.Validation(ChildNotOwnedMessage)
	}
	parentUnit, ok := domain.CanonicalUnitID(parentUnitID)
	if !ok {
		return nil, apperror.Validation(ParentNotOwnedMessage)
	}
	child, err := f.requireOwnedUnit(ctx, customerID, childUnit, ChildNotOwnedMessage)
	if err != nil {
		return nil, err
	}
	if _, err := f.requireOwnedUnit(ctx, customerID, parentUnit, ParentNotOwnedMessage); err != nil {
		return nil, err
	}
	ids, err := f.childEligibilityIDs(ctx, child.ProductID)
	if err != nil {
		return nil, err
	}
	if err := f.CheckPurchaseAll(ctx, customerID, ids, catalog.AssignmentContext{ParentUnitID: parentUnit}); err != nil {
		return nil, err
	}
	current, err := f.assignments.CurrentByChild(ctx, childUnit)
	if err != nil {
		return nil, err
	}
	if current != nil && current.Status() == domain.StatusAssigned {
		return nil, apperror.Conflict(AlreadyAssignedMessage)
	}
	if current != nil {
		return f.assignments.Assign(ctx, current.ID, parentUnit)
	}
	return f.assignments.CreateAssigned(ctx, parentUnit, childUnit)
}

// CurrentOwned returns the live assignment for a child unit the customer owns.
func (f *Flow) CurrentOwned(ctx context.Context, customerID, childUnitID string) (*domain.ProductAssignment, error) {
	if f == nil {
		return nil, fmt.Errorf("assignment flow: not configured")
	}
	childUnit, ok := domain.CanonicalUnitID(childUnitID)
	if !ok {
		return nil, apperror.Validation(ChildNotOwnedMessage)
	}
	if _, err := f.requireOwnedUnit(ctx, strings.TrimSpace(customerID), childUnit, ChildNotOwnedMessage); err != nil {
		return nil, err
	}
	return f.assignments.CurrentByChild(ctx, childUnit)
}

// ListOwnedParents returns owned order lines that are eligible parents
// for childProductID. Unpaid, pending, cancelled, and failed orders
// are skipped.
func (f *Flow) ListOwnedParents(ctx context.Context, customerID, childProductID string) ([]OwnedParent, error) {
	if f == nil {
		return nil, fmt.Errorf("assignment flow: not configured")
	}
	customerID = strings.TrimSpace(customerID)
	childProductID = strings.TrimSpace(childProductID)
	if customerID == "" {
		return nil, apperror.Validation(ParentUnitSignInMessage)
	}
	if childProductID == "" {
		return nil, apperror.Validation("child product id must not be empty")
	}
	leafIDs, err := f.childEligibilityIDs(ctx, childProductID)
	if err != nil {
		return nil, err
	}
	orders, err := f.orders.FindByCustomerID(ctx, customerID)
	if err != nil {
		return nil, fmt.Errorf("assignment flow: list orders: %w", err)
	}
	type parentLine struct {
		order order.Order
		item  order.Item
	}
	pending := make([]order.Order, 0, len(orders))
	orderIDs := make([]string, 0, len(orders))
	for i := range orders {
		o := orders[i]
		if strings.TrimSpace(o.CustomerID) != customerID {
			continue
		}
		if o.Status() == order.OrderStatusCancelled || o.Status() == order.OrderStatusFailed {
			continue
		}
		pending = append(pending, o)
		orderIDs = append(orderIDs, o.ID)
	}
	byPayment, err := f.lookupPayments(ctx, orderIDs)
	if err != nil {
		return nil, err
	}
	variantIDs := make([]string, 0)
	candidates := make([]parentLine, 0)
	for i := range pending {
		o := pending[i]
		owned, err := f.orderOwnedWithPayment(&o, byPayment[o.ID])
		if err != nil {
			return nil, err
		}
		if !owned {
			continue
		}
		for _, item := range o.Items() {
			candidates = append(candidates, parentLine{order: o, item: item})
			variantIDs = append(variantIDs, item.VariantID)
		}
	}
	byVariant, err := f.lookupVariants(ctx, variantIDs)
	if err != nil {
		return nil, err
	}
	eligible := make(map[string]bool)
	out := make([]OwnedParent, 0)
	for _, cand := range candidates {
		v := byVariant[cand.item.VariantID]
		if v == nil || strings.TrimSpace(v.ProductID) == "" {
			continue
		}
		ok, seen := eligible[v.ProductID]
		if !seen {
			ok, err = f.parentEligibleForLeaves(ctx, leafIDs, v.ProductID)
			if err != nil {
				return nil, err
			}
			eligible[v.ProductID] = ok
		}
		if !ok {
			continue
		}
		out = append(out, OwnedParent{
			UnitID:    domain.UnitID(cand.order.ID, cand.item.VariantID),
			OrderID:   cand.order.ID,
			VariantID: cand.item.VariantID,
			ProductID: v.ProductID,
			Name:      cand.item.Name,
		})
	}
	return out, nil
}

func (f *Flow) resolveParentProduct(ctx context.Context, customerID string, ac catalog.AssignmentContext) (string, error) {
	if ac.ParentUnitID == "" {
		return ac.ParentProductID, nil
	}
	if customerID == "" {
		return "", apperror.Validation(ParentUnitSignInMessage)
	}
	owned, err := f.requireOwnedUnit(ctx, customerID, ac.ParentUnitID, ParentNotOwnedMessage)
	if err != nil {
		return "", err
	}
	if ac.ParentProductID != "" && ac.ParentProductID != owned.ProductID {
		return "", apperror.Validation(ParentUnitMismatchMessage)
	}
	return owned.ProductID, nil
}

type ownedUnit struct {
	OrderID   string
	VariantID string
	ProductID string
}

func (f *Flow) childEligibilityIDs(ctx context.Context, productID string) ([]string, error) {
	productID = strings.TrimSpace(productID)
	if productID == "" {
		return nil, nil
	}
	if f.products == nil {
		return []string{productID}, nil
	}
	p, err := f.products.FindByID(ctx, productID)
	if err != nil {
		return nil, fmt.Errorf("assignment flow: lookup product: %w", err)
	}
	if p == nil {
		return []string{productID}, nil
	}
	ids, err := catalogapp.LinkedParentChildProductIDs(ctx, f.products, f.components, p)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []string{productID}, nil
	}
	return ids, nil
}

func (f *Flow) parentEligibleForLeaves(ctx context.Context, leafIDs []string, parentProductID string) (bool, error) {
	if len(leafIDs) == 0 {
		return false, nil
	}
	for _, leafID := range leafIDs {
		ok, err := f.eligibility.IsEligible(ctx, leafID, parentProductID)
		if err != nil {
			if apperror.Is(err, apperror.CodeValidation) {
				return false, err
			}
			return false, fmt.Errorf("assignment flow: eligibility: %w", err)
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

func (f *Flow) requireOwnedUnit(ctx context.Context, customerID, unitID, notOwned string) (ownedUnit, error) {
	unitID, ok := domain.CanonicalUnitID(unitID)
	if !ok {
		return ownedUnit{}, apperror.Validation(notOwned)
	}
	orderID, variantID, ok := domain.SplitUnitID(unitID)
	if !ok {
		return ownedUnit{}, apperror.Validation(notOwned)
	}
	if strings.TrimSpace(customerID) == "" {
		return ownedUnit{}, apperror.Validation(ParentUnitSignInMessage)
	}
	o, err := f.orders.FindByID(ctx, orderID)
	if err != nil {
		return ownedUnit{}, fmt.Errorf("assignment flow: lookup order: %w", err)
	}
	if o == nil || strings.TrimSpace(o.CustomerID) != customerID {
		return ownedUnit{}, apperror.Validation(notOwned)
	}
	owned, err := f.orderUnitOwned(ctx, o)
	if err != nil {
		return ownedUnit{}, err
	}
	if !owned {
		return ownedUnit{}, apperror.Validation(notOwned)
	}
	found := false
	for _, item := range o.Items() {
		if item.VariantID == variantID {
			found = true
			break
		}
	}
	if !found {
		return ownedUnit{}, apperror.Validation(notOwned)
	}
	v, err := f.variants.FindByID(ctx, variantID)
	if err != nil {
		return ownedUnit{}, fmt.Errorf("assignment flow: lookup variant: %w", err)
	}
	if v == nil || strings.TrimSpace(v.ProductID) == "" {
		return ownedUnit{}, apperror.Validation(notOwned)
	}
	return ownedUnit{OrderID: orderID, VariantID: variantID, ProductID: v.ProductID}, nil
}

func (f *Flow) orderUnitOwned(ctx context.Context, o *order.Order) (bool, error) {
	if o == nil {
		return false, nil
	}
	if o.Status() == order.OrderStatusCancelled || o.Status() == order.OrderStatusFailed {
		return false, nil
	}
	byPayment, err := f.lookupPayments(ctx, []string{o.ID})
	if err != nil {
		return false, err
	}
	return f.orderOwnedWithPayment(o, byPayment[o.ID])
}

func (f *Flow) orderOwnedWithPayment(o *order.Order, py *payment.Payment) (bool, error) {
	if o == nil {
		return false, nil
	}
	if o.Status() == order.OrderStatusCancelled || o.Status() == order.OrderStatusFailed {
		return false, nil
	}
	if py != nil {
		return py.Status() == payment.StatusCompleted, nil
	}
	payable, err := o.PayableAmount()
	if err != nil {
		return false, fmt.Errorf("assignment flow: payable amount: %w", err)
	}
	return payable.Amount() == 0, nil
}

func (f *Flow) lookupPayments(ctx context.Context, ids []string) (map[string]*payment.Payment, error) {
	unique := uniqueNonEmpty(ids)
	if len(unique) == 0 {
		return map[string]*payment.Payment{}, nil
	}
	if batch, ok := f.payments.(PaymentBatchLookup); ok {
		found, err := batch.FindByOrderIDs(ctx, unique)
		if err != nil {
			return nil, fmt.Errorf("assignment flow: lookup payments: %w", err)
		}
		if found == nil {
			return map[string]*payment.Payment{}, nil
		}
		return found, nil
	}
	out := make(map[string]*payment.Payment, len(unique))
	for _, id := range unique {
		py, err := f.payments.FindByOrderID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("assignment flow: lookup payment %s: %w", id, err)
		}
		if py != nil {
			out[id] = py
		}
	}
	return out, nil
}

func (f *Flow) lookupVariants(ctx context.Context, ids []string) (map[string]*catalog.Variant, error) {
	unique := uniqueNonEmpty(ids)
	if len(unique) == 0 {
		return map[string]*catalog.Variant{}, nil
	}
	if batch, ok := f.variants.(VariantBatchLookup); ok {
		found, err := batch.FindByIDs(ctx, unique)
		if err != nil {
			return nil, fmt.Errorf("assignment flow: lookup variants: %w", err)
		}
		if found == nil {
			return map[string]*catalog.Variant{}, nil
		}
		return found, nil
	}
	out := make(map[string]*catalog.Variant, len(unique))
	for _, id := range unique {
		v, err := f.variants.FindByID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("assignment flow: lookup variant %s: %w", id, err)
		}
		if v != nil {
			out[id] = v
		}
	}
	return out, nil
}

func uniqueNonEmpty(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
