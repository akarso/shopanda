package assignment

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	catalogapp "github.com/akarso/shopanda/internal/application/catalog"
	domain "github.com/akarso/shopanda/internal/domain/assignment"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
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
	// ChildUnitInvalidMessage is returned when an admin unit id is not
	// orderID/variantID.
	ChildUnitInvalidMessage = "assignment child unit id is invalid"
	// ParentUnitInvalidMessage is returned when an admin parent unit id
	// is not orderID/variantID.
	ParentUnitInvalidMessage = "assignment parent unit id is invalid"
	// ChildUnitMissingMessage is returned when admin assign cannot
	// resolve the child order line.
	ChildUnitMissingMessage = "assignment child unit was not found"
	// ParentUnitMissingMessage is returned when admin assign cannot
	// resolve the parent order line.
	ParentUnitMissingMessage = "assignment parent unit was not found"
	// ChildVariantMissingMessage is returned when the child order line
	// exists but its catalog variant does not.
	ChildVariantMissingMessage = "assignment child catalog variant was not found"
	// ParentVariantMissingMessage is returned when the parent order line
	// exists but its catalog variant does not.
	ParentVariantMissingMessage = "assignment parent catalog variant was not found"
	// ChildProductMissingMessage is returned when the child variant's
	// product cannot be loaded.
	ChildProductMissingMessage = "assignment child product was not found"
	// ChildNotLinkedMessage is returned when a new bind's child product
	// does not require a linked parent and has no such required leaf.
	ChildNotLinkedMessage = "assignment child does not require a linked parent"
	// ChildOrderNotBindableMessage is returned when the child order is
	// cancelled, failed, or unpaid and the request did not force it.
	ChildOrderNotBindableMessage = "assignment child order is not bindable without force and a reason"
	// ParentOrderNotBindableMessage is returned when the parent order is
	// cancelled, failed, or unpaid and the request did not force it.
	ParentOrderNotBindableMessage = "assignment parent order is not bindable without force and a reason"
	// ForceReasonRequiredMessage is returned when force is set without a reason.
	ForceReasonRequiredMessage = "reason is required when force is true"
	// ForceNotApplicableMessage is returned when force is set and both
	// orders are already bindable. The override is not stored.
	ForceNotApplicableMessage = "force not applicable"
	// ParentHasAssignedChildrenMessage is returned when a return targets
	// a parent unit that still has live assigned children (PR-1072).
	ParentHasAssignedChildrenMessage = "cannot return a parent unit that still has assigned children; revoke or return those children first"
	// UnitOnOpenReturnMessage is returned when AssignOwned / AdminAssign /
	// CheckPurchase / RecordPurchase targets a unit that appears on an
	// open or refunded return (PR-1072).
	UnitOnOpenReturnMessage = "cannot assign a unit that is on an open or refunded return"
	// RevokeReasonReturnApproved is the assignment.revoked reason when
	// returns Approve revokes the child unit.
	RevokeReasonReturnApproved = "return_approved"
	// RevokeReasonReturnRefunded is the assignment.revoked reason when
	// returns Refund revokes the child unit.
	RevokeReasonReturnRefunded = "return_refunded"
)

// OpenReturnChecker blocks bind when a unit is on an open or refunded
// RMA (requested / approved / received / refunded). Rejected and
// cancelled do not block. returns.Service implements it.
type OpenReturnChecker interface {
	AssertNotOnOpenReturn(ctx context.Context, unitID string) error
	// UnitsOnBlockingReturn reports which of unitIDs are on an open or
	// refunded return. Implementations load once per unique order ID.
	// Empty/unparseable IDs are omitted (same skip as AssertNotOnOpenReturn).
	UnitsOnBlockingReturn(ctx context.Context, unitIDs []string) (map[string]struct{}, error)
}

// TxStarter begins the transaction used to lock units and persist an
// assignment write. *sql.DB satisfies it.
type TxStarter interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

const assignmentUnitLockPrefix = "assignment:unit:"

// LockAssignmentUnits acquires transaction-scoped advisory locks for each
// unit id (sorted) so assign writes and return mutations serialize on the
// same keys. No-op when tx is nil. Shared by assignment.Flow and
// returns.Service (PR-1072 race fix).
func LockAssignmentUnits(ctx context.Context, tx *sql.Tx, unitIDs ...string) error {
	if tx == nil {
		return nil
	}
	keys := make([]string, 0, len(unitIDs))
	seen := make(map[string]struct{}, len(unitIDs))
	for _, unitID := range unitIDs {
		unitID = strings.TrimSpace(unitID)
		if unitID == "" {
			continue
		}
		if canon, ok := domain.CanonicalUnitID(unitID); ok {
			unitID = canon
		}
		if _, ok := seen[unitID]; ok {
			continue
		}
		seen[unitID] = struct{}{}
		keys = append(keys, assignmentUnitLockPrefix+unitID)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key); err != nil {
			return fmt.Errorf("assignment flow: unit lock: %w", err)
		}
	}
	return nil
}

// AdminAssignRequest is an operator bind. Force with a non-empty reason
// overrides a cancelled, failed, or unpaid parent or child order. The
// reason is stored on the assignment row (forced_at, forced_reason) only
// when a side is not bindable, returned as `reason`, and written to the
// audit entry. force on an already-bindable pair is rejected.
type AdminAssignRequest struct {
	ChildUnitID  string
	ParentUnitID string
	Force        bool
	Reason       string
}

// UnitFacts is the order context returned with an admin bind so a later
// reader can see which orders were involved.
type UnitFacts struct {
	CustomerID    string
	OrderStatus   string
	PaymentStatus string
}

// AdminAssignResult is a successful operator bind. Created is true when
// a new assigned row was inserted. It is false when an unassigned row
// was updated.
type AdminAssignResult struct {
	Assignment *domain.ProductAssignment
	Created    bool
	Reason     string
	Child      UnitFacts
	Parent     UnitFacts
}

type orderBindFacts struct {
	UnitFacts
	Bindable bool
}

// Eligibility is the catalog-rule predicate used when a parent is present.
type Eligibility interface {
	IsEligible(ctx context.Context, childID, parentID string) (bool, error)
}

// OrderLookup is the order reads Flow needs for ownership.
type OrderLookup interface {
	FindByID(ctx context.Context, id string) (*order.Order, error)
	FindByCustomerID(ctx context.Context, customerID string) ([]order.Order, error)
}

// OrderBatchLookup is an optional batch read. postgres OrderRepository
// implements it so FactsForUnits does not FindByID once per order.
type OrderBatchLookup interface {
	FindByIDs(ctx context.Context, ids []string) (map[string]*order.Order, error)
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
// purchase, and post-purchase Assign. AdminAssign (PR-1071) is the
// operator path. It does not require both units to belong to one
// customer. Cancelled, failed, and unpaid orders are refused unless
// the request sets force and a reason. A new row also requires
// RequiresLinkedParent (or a required linked leaf); an existing
// unassigned row may be assigned without that flag. State transitions
// stay on Service. OpenReturnChecker (PR-1072) blocks bind while a
// unit is on an open or refunded return. Optional TxStarter serializes
// assign writes with return mutations via LockAssignmentUnits.
type Flow struct {
	assignments *Service
	repo        domain.Repository
	eligibility Eligibility
	orders      OrderLookup
	variants    VariantLookup
	payments    PaymentLookup
	products    ProductLookup
	components  BundleComponentLookup
	openReturns OpenReturnChecker
	txStarter   TxStarter
	writeTx     *sql.Tx
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

// WithOpenReturnChecker blocks AssignOwned / AdminAssign / CheckPurchase /
// RecordPurchase when the child or parent unit is on an open or refunded
// return (PR-1072). ListOwnedParents also omits those parents.
// Nil skips the gate (tests without returns).
func (f *Flow) WithOpenReturnChecker(checker OpenReturnChecker) *Flow {
	if f == nil {
		return nil
	}
	f.openReturns = checker
	return f
}

// WithTxStarter enables AssignOwned / AdminAssign / RecordPurchase to
// persist under LockAssignmentUnits + an open-return recheck in one TX
// (PR-1072). Nil keeps the fail-fast check only (unit tests without DB).
func (f *Flow) WithTxStarter(starter TxStarter) *Flow {
	if f == nil {
		return nil
	}
	f.txStarter = starter
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
	cp.writeTx = tx
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
	// Parent unit on an open RMA must not accept a new stamped ticket
	// at cart/checkout (same gate as AssignOwned).
	if ac.ParentUnitID != "" {
		if err := f.assertUnitsNotOnOpenReturn(ctx, ac.ParentUnitID); err != nil {
			return err
		}
	}
	return nil
}

// RecordPurchase writes one ProductAssignment per RequiresLinkedParent
// line. A parent unit becomes CreateAssigned; deferred or product-only
// becomes CreateUnassigned. This is the only checkout write policy.
//
// Assigned lines lock every parent+child unit id once (sorted inside
// LockAssignmentUnits) before any CreateAssigned write. Checkout holds
// one TX across the batch; locking line-by-line in line order can
// deadlock two concurrent checkouts that name the same parents in
// opposite order.
func (f *Flow) RecordPurchase(ctx context.Context, customerID, orderID string, lines []PurchaseLine) error {
	if f == nil {
		return fmt.Errorf("assignment flow: not configured")
	}
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return apperror.Validation("order id must not be empty")
	}
	customerID = strings.TrimSpace(customerID)

	type assignedWrite struct {
		parent string
		child  string
	}
	assigned := make([]assignedWrite, 0, len(lines))
	unassigned := make([]string, 0, len(lines))
	lockPairs := make([][2]string, 0, len(lines))

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
			// Fail-fast open-return check; recheck again under the batch
			// lock inside runAssignedWrite (same TX as checkout when WithTx'd).
			if err := f.assertUnitsNotOnOpenReturn(ctx, canonical, childUnit); err != nil {
				return err
			}
			assigned = append(assigned, assignedWrite{parent: canonical, child: childUnit})
			lockPairs = append(lockPairs, [2]string{canonical, childUnit})
			continue
		}
		unassigned = append(unassigned, childUnit)
	}

	writeLines := func(ctx context.Context, bound *Flow) error {
		for _, a := range assigned {
			if _, err := bound.assignments.CreateAssigned(ctx, a.parent, a.child); err != nil {
				return err
			}
		}
		for _, childUnit := range unassigned {
			if _, err := bound.assignments.CreateUnassigned(ctx, childUnit); err != nil {
				return err
			}
		}
		return nil
	}

	if len(assigned) == 0 {
		return writeLines(ctx, f)
	}

	lockIDs := assignedPurchaseLockUnitIDs(lockPairs)
	_, err := f.runAssignedWrite(ctx, lockIDs, func(ctx context.Context, bound *Flow) (*domain.ProductAssignment, error) {
		if err := writeLines(ctx, bound); err != nil {
			return nil, err
		}
		return nil, nil
	})
	return err
}

// assignedPurchaseLockUnitIDs flattens parent+child unit pairs for a
// RecordPurchase batch. LockAssignmentUnits sorts and dedups so the
// full set is ordered once before any line write.
func assignedPurchaseLockUnitIDs(pairs [][2]string) []string {
	out := make([]string, 0, len(pairs)*2)
	for _, p := range pairs {
		if p[0] != "" {
			out = append(out, p[0])
		}
		if p[1] != "" {
			out = append(out, p[1])
		}
	}
	return out
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
		if _, err := f.RevokeForUnit(ctx, childUnit, ""); err != nil {
			return err
		}
	}
	return nil
}

// AssertParentReturnable fails when parentUnitID still has live
// assigned children that are not listed in skipChildUnitIDs. Returning
// the card while other tickets remain assigned is blocked (PR-1072).
// Children that are themselves lines on the same return request are
// passed as skipChildUnitIDs so a same-order "return card and its
// tickets" RMA is allowed. A parent with only revoked history, or no
// rows, is returnable. The validation message lists the required child
// unit ids so a card-only submit can show which ticket lines to add.
func (f *Flow) AssertParentReturnable(ctx context.Context, parentUnitID string, skipChildUnitIDs ...string) error {
	if f == nil {
		return fmt.Errorf("assignment flow: not configured")
	}
	skip := make(map[string]struct{}, len(skipChildUnitIDs))
	for _, id := range skipChildUnitIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if canon, ok := domain.CanonicalUnitID(id); ok {
			id = canon
		}
		skip[id] = struct{}{}
	}
	blocking := make([]string, 0)
	seenBlock := make(map[string]struct{})
	offset := 0
	for {
		rows, hasMore, err := f.assignments.ListByParent(ctx, parentUnitID, ParentListQuery{
			Status: ParentListStatusAssigned,
			Limit:  ParentListMaxLimit,
			Offset: offset,
		})
		if err != nil {
			return err
		}
		for _, row := range rows {
			child := row.ChildOrderItemID
			if canon, ok := domain.CanonicalUnitID(child); ok {
				child = canon
			}
			if _, ok := skip[child]; ok {
				continue
			}
			if _, ok := seenBlock[child]; ok {
				continue
			}
			seenBlock[child] = struct{}{}
			blocking = append(blocking, child)
		}
		if !hasMore || len(rows) == 0 {
			break
		}
		offset += len(rows)
	}
	if len(blocking) == 0 {
		return nil
	}
	return apperror.Validation(fmt.Sprintf("%s (required: %s)", ParentHasAssignedChildrenMessage, strings.Join(blocking, ", ")))
}

// RevokeForUnit revokes the current (unassigned or assigned) row for
// childUnitID when one exists. Missing rows return (nil, nil). An
// already-revoked current is not expected (CurrentByChild skips them);
// a successful revoke returns the revoked row. reason is a stable token
// for callers that publish assignment.revoked (e.g. return_approved).
// Used by returns on approve/refund (PR-1072) and by RevokeForOrder.
func (f *Flow) RevokeForUnit(ctx context.Context, childUnitID, reason string) (*domain.ProductAssignment, error) {
	if f == nil {
		return nil, fmt.Errorf("assignment flow: not configured")
	}
	_ = reason // caller publishes; kept on the signature for audit context
	current, err := f.assignments.CurrentByChild(ctx, childUnitID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, nil
	}
	revoked, _, err := f.assignments.Revoke(ctx, current.ID)
	if err != nil {
		return nil, err
	}
	return revoked, nil
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
	if err := f.assertUnitsNotOnOpenReturn(ctx, childUnit, parentUnit); err != nil {
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
	currentID := ""
	if current != nil {
		currentID = current.ID
	}
	return f.runAssignedWrite(ctx, []string{childUnit, parentUnit}, func(ctx context.Context, bound *Flow) (*domain.ProductAssignment, error) {
		if currentID != "" {
			return bound.assignments.Assign(ctx, currentID, parentUnit)
		}
		return bound.assignments.CreateAssigned(ctx, parentUnit, childUnit)
	})
}

// AdminAssign binds a child unit to a parent unit for an operator.
// Canonical unit ids reject a non-UUID order part before any order read.
// The order line must exist; a missing catalog variant is a different
// error from a missing unit. A new row requires RequiresLinkedParent (or
// a required linked leaf). An existing unassigned row may be assigned
// without that flag; eligibility then uses the product on the order line.
// A live assigned row is a conflict. Cancelled, failed, and unpaid orders
// are refused unless force is true and reason is non-empty. Force is
// stored only when a side is not bindable. force on an already-bindable
// pair is rejected. Customer id is not an ownership check.
func (f *Flow) AdminAssign(ctx context.Context, req AdminAssignRequest) (*AdminAssignResult, error) {
	if f == nil {
		return nil, fmt.Errorf("assignment flow: not configured")
	}
	reason := strings.TrimSpace(req.Reason)
	if req.Force && reason == "" {
		return nil, apperror.Validation(ForceReasonRequiredMessage)
	}
	if req.Force && utf8.RuneCountInString(reason) > domain.MaxForcedReasonLen {
		return nil, apperror.Validation(domain.ForcedReasonTooLongMessage)
	}
	childUnit, ok := domain.CanonicalUnitID(req.ChildUnitID)
	if !ok {
		return nil, apperror.Validation(ChildUnitInvalidMessage)
	}
	parentUnit, ok := domain.CanonicalUnitID(req.ParentUnitID)
	if !ok {
		return nil, apperror.Validation(ParentUnitInvalidMessage)
	}
	childResolved, err := f.resolveAdminUnit(ctx, childUnit, ChildUnitMissingMessage, ChildVariantMissingMessage)
	if err != nil {
		return nil, err
	}
	parentResolved, err := f.resolveAdminUnit(ctx, parentUnit, ParentUnitMissingMessage, ParentVariantMissingMessage)
	if err != nil {
		return nil, err
	}
	if err := f.assertUnitsNotOnOpenReturn(ctx, childUnit, parentUnit); err != nil {
		return nil, err
	}
	childFacts, err := f.unitOrderFacts(ctx, childResolved.Order)
	if err != nil {
		return nil, err
	}
	parentFacts, err := f.unitOrderFacts(ctx, parentResolved.Order)
	if err != nil {
		return nil, err
	}
	if err := refuseUnbindable(childFacts, req.Force, ChildOrderNotBindableMessage); err != nil {
		return nil, err
	}
	if err := refuseUnbindable(parentFacts, req.Force, ParentOrderNotBindableMessage); err != nil {
		return nil, err
	}
	needsForce := !childFacts.Bindable || !parentFacts.Bindable
	if req.Force && !needsForce {
		return nil, apperror.Validation(ForceNotApplicableMessage)
	}
	current, err := f.assignments.CurrentByChild(ctx, childUnit)
	if err != nil {
		return nil, err
	}
	if current != nil && current.Status() == domain.StatusAssigned {
		return nil, apperror.Conflict(AlreadyAssignedMessage)
	}
	if current == nil {
		if err := f.requireChildLinkedParent(ctx, childResolved.ProductID); err != nil {
			return nil, err
		}
	}
	ids, err := f.childEligibilityIDs(ctx, childResolved.ProductID)
	if err != nil {
		return nil, err
	}
	// A current unassigned row was written at purchase, when the product
	// required a linked parent. Clearing that flag later must not block
	// the bind. Eligibility then uses the product on the order line.
	if current != nil && len(ids) == 0 && strings.TrimSpace(childResolved.ProductID) != "" {
		ids = []string{childResolved.ProductID}
	}
	eligible, err := f.parentEligibleForLeaves(ctx, ids, parentResolved.ProductID)
	if err != nil {
		return nil, err
	}
	if !eligible {
		return nil, apperror.Validation(ParentNotEligibleMessage)
	}
	currentID := ""
	if current != nil {
		currentID = current.ID
	}
	created := currentID == ""
	saved, err := f.runAssignedWrite(ctx, []string{childUnit, parentUnit}, func(ctx context.Context, bound *Flow) (*domain.ProductAssignment, error) {
		if currentID != "" {
			if needsForce {
				return bound.assignments.AssignForced(ctx, currentID, parentUnit, reason)
			}
			return bound.assignments.Assign(ctx, currentID, parentUnit)
		}
		if needsForce {
			return bound.assignments.CreateAssignedForced(ctx, parentUnit, childUnit, reason)
		}
		return bound.assignments.CreateAssigned(ctx, parentUnit, childUnit)
	})
	if err != nil {
		return nil, err
	}
	storedReason := ""
	if needsForce {
		storedReason = reason
	}
	return &AdminAssignResult{
		Assignment: saved,
		Created:    created,
		Reason:     storedReason,
		Child:      childFacts.UnitFacts,
		Parent:     parentFacts.UnitFacts,
	}, nil
}

type resolvedAdminUnit struct {
	Order     *order.Order
	ProductID string
}

// resolveAdminUnit confirms the order and the order line, then loads the
// product id from the line's catalog variant. A missing order or line is
// missingUnit. A missing variant is missingVariant.
func (f *Flow) resolveAdminUnit(ctx context.Context, unitID, missingUnit, missingVariant string) (resolvedAdminUnit, error) {
	orderID, variantID, ok := domain.SplitUnitID(unitID)
	if !ok {
		return resolvedAdminUnit{}, apperror.Validation(missingUnit)
	}
	o, err := f.orders.FindByID(ctx, orderID)
	if err != nil {
		if apperror.Is(err, apperror.CodeValidation) {
			return resolvedAdminUnit{}, apperror.Validation(missingUnit)
		}
		return resolvedAdminUnit{}, fmt.Errorf("assignment flow: lookup order: %w", err)
	}
	if o == nil {
		return resolvedAdminUnit{}, apperror.Validation(missingUnit)
	}
	found := false
	for _, item := range o.Items() {
		if item.VariantID == variantID {
			found = true
			break
		}
	}
	if !found {
		return resolvedAdminUnit{}, apperror.Validation(missingUnit)
	}
	v, err := f.variants.FindByID(ctx, variantID)
	if err != nil {
		if apperror.Is(err, apperror.CodeValidation) {
			return resolvedAdminUnit{}, apperror.Validation(missingVariant)
		}
		return resolvedAdminUnit{}, fmt.Errorf("assignment flow: lookup variant: %w", err)
	}
	if v == nil || strings.TrimSpace(v.ProductID) == "" {
		return resolvedAdminUnit{}, apperror.Validation(missingVariant)
	}
	return resolvedAdminUnit{Order: o, ProductID: v.ProductID}, nil
}

func (f *Flow) requireChildLinkedParent(ctx context.Context, productID string) error {
	if f.products == nil {
		return apperror.Validation(ChildProductMissingMessage)
	}
	p, err := f.products.FindByID(ctx, productID)
	if err != nil {
		if apperror.Is(err, apperror.CodeValidation) {
			return apperror.Validation(ChildProductMissingMessage)
		}
		return fmt.Errorf("assignment flow: lookup product: %w", err)
	}
	if p == nil {
		return apperror.Validation(ChildProductMissingMessage)
	}
	if p.RequiresLinkedParent {
		return nil
	}
	ids, err := catalogapp.LinkedParentChildProductIDs(ctx, f.products, f.components, p)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return apperror.Validation(ChildNotLinkedMessage)
	}
	return nil
}

func (f *Flow) unitOrderFacts(ctx context.Context, o *order.Order) (orderBindFacts, error) {
	facts := orderBindFacts{UnitFacts: UnitFacts{
		CustomerID:  strings.TrimSpace(o.CustomerID),
		OrderStatus: string(o.Status()),
	}}
	byPayment, err := f.lookupPayments(ctx, []string{o.ID})
	if err != nil {
		return facts, err
	}
	py := byPayment[o.ID]
	if py != nil {
		facts.PaymentStatus = string(py.Status())
	}
	ok, err := f.orderOwnedWithPayment(o, py)
	if err != nil {
		return facts, err
	}
	facts.Bindable = ok
	return facts, nil
}

func refuseUnbindable(facts orderBindFacts, force bool, msg string) error {
	if facts.Bindable || force {
		return nil
	}
	return apperror.Validation(fmt.Sprintf("%s (order_status=%s, payment_status=%s)", msg, facts.OrderStatus, facts.PaymentStatus))
}

func (f *Flow) assertUnitsNotOnOpenReturn(ctx context.Context, unitIDs ...string) error {
	if f == nil || f.openReturns == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(unitIDs))
	for _, unitID := range unitIDs {
		unitID = strings.TrimSpace(unitID)
		if unitID == "" {
			continue
		}
		if canon, ok := domain.CanonicalUnitID(unitID); ok {
			unitID = canon
		}
		if _, ok := seen[unitID]; ok {
			continue
		}
		seen[unitID] = struct{}{}
		if err := f.openReturns.AssertNotOnOpenReturn(ctx, unitID); err != nil {
			return err
		}
	}
	return nil
}

// unitsOnBlockingReturn batches the open/refunded-RMA filter for
// ListOwnedParents (one load per unique order ID via the checker).
func (f *Flow) unitsOnBlockingReturn(ctx context.Context, unitIDs []string) (map[string]struct{}, error) {
	if f == nil || f.openReturns == nil {
		return map[string]struct{}{}, nil
	}
	return f.openReturns.UnitsOnBlockingReturn(ctx, unitIDs)
}

// runAssignedWrite serializes a bind against return mutations: optional
// advisory locks on the unit ids, open-return recheck, then write on a
// TX-bound flow. When already inside WithTx (checkout), reuses that TX.
// Without a tx starter (unit tests), still rechecks immediately before write.
func (f *Flow) runAssignedWrite(
	ctx context.Context,
	unitIDs []string,
	write func(ctx context.Context, bound *Flow) (*domain.ProductAssignment, error),
) (*domain.ProductAssignment, error) {
	execute := func(ctx context.Context, bound *Flow, tx *sql.Tx) (*domain.ProductAssignment, error) {
		if err := LockAssignmentUnits(ctx, tx, unitIDs...); err != nil {
			return nil, err
		}
		if err := bound.assertUnitsNotOnOpenReturn(ctx, unitIDs...); err != nil {
			return nil, err
		}
		return write(ctx, bound)
	}
	if f.writeTx != nil {
		return execute(ctx, f, f.writeTx)
	}
	if f.txStarter != nil && f.BindsTx() {
		tx, err := f.txStarter.BeginTx(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("assignment flow: begin tx: %w", err)
		}
		defer tx.Rollback()
		bound := f.WithTx(tx)
		saved, err := execute(ctx, bound, tx)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("assignment flow: commit: %w", err)
		}
		return saved, nil
	}
	if err := f.assertUnitsNotOnOpenReturn(ctx, unitIDs...); err != nil {
		return nil, err
	}
	return write(ctx, f)
}

// FactsForUnits resolves live order context for unit ids (orderID/variantID).
// The result is keyed by CanonicalUnitID, not the caller's original string.
// A missing order yields no entry. Callers use it on list and current
// reads so a cancelled or unpaid unit stays visible after the bind.
// A UUID order id is lowercased before the order map lookup so an
// uppercase unit hits the lowercase id stored on the order.
func (f *Flow) FactsForUnits(ctx context.Context, unitIDs []string) (map[string]UnitFacts, error) {
	if f == nil {
		return nil, fmt.Errorf("assignment flow: not configured")
	}
	type unitOrder struct {
		unit    string
		orderID string
	}
	pairs := make([]unitOrder, 0, len(unitIDs))
	orderIDs := make([]string, 0, len(unitIDs))
	seenOrder := make(map[string]struct{}, len(unitIDs))
	for _, unit := range unitIDs {
		unit = strings.TrimSpace(unit)
		if unit == "" {
			continue
		}
		orderID, _, ok := domain.SplitUnitID(unit)
		if !ok {
			continue
		}
		// Key the result by the canonical unit id. FindByIDs keys by the
		// stored order id, which is the lowercase UUID minted at purchase.
		key := unit
		if canon, ok := domain.CanonicalUnitID(unit); ok {
			key = canon
			if oid, _, splitOK := domain.SplitUnitID(canon); splitOK {
				orderID = oid
			}
		}
		pairs = append(pairs, unitOrder{unit: key, orderID: orderID})
		if _, ok := seenOrder[orderID]; ok {
			continue
		}
		seenOrder[orderID] = struct{}{}
		orderIDs = append(orderIDs, orderID)
	}
	orders, err := f.lookupOrders(ctx, orderIDs)
	if err != nil {
		return nil, err
	}
	payIDs := make([]string, 0, len(orderIDs))
	for _, orderID := range orderIDs {
		if orders[orderID] != nil {
			payIDs = append(payIDs, orders[orderID].ID)
		}
	}
	byPayment, err := f.lookupPayments(ctx, payIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]UnitFacts, len(pairs))
	for _, pair := range pairs {
		o := orders[pair.orderID]
		if o == nil {
			continue
		}
		facts := UnitFacts{
			CustomerID:  strings.TrimSpace(o.CustomerID),
			OrderStatus: string(o.Status()),
		}
		if py := byPayment[o.ID]; py != nil {
			facts.PaymentStatus = string(py.Status())
		}
		out[pair.unit] = facts
	}
	return out, nil
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
		unitID := domain.UnitID(cand.order.ID, cand.item.VariantID)
		out = append(out, OwnedParent{
			UnitID:    unitID,
			OrderID:   cand.order.ID,
			VariantID: cand.item.VariantID,
			ProductID: v.ProductID,
			Name:      cand.item.Name,
		})
	}
	// Hide cards on an open or refunded RMA so account assign UI does
	// not offer a parent that AssignOwned would 422 (PR-1072). Check once
	// per unique order ID rather than once per candidate line.
	blocked, err := f.unitsOnBlockingReturn(ctx, ownedParentUnitIDs(out))
	if err != nil {
		return nil, err
	}
	if len(blocked) == 0 {
		return out, nil
	}
	filtered := make([]OwnedParent, 0, len(out))
	for _, p := range out {
		if _, skip := blocked[p.UnitID]; skip {
			continue
		}
		filtered = append(filtered, p)
	}
	return filtered, nil
}

func ownedParentUnitIDs(parents []OwnedParent) []string {
	ids := make([]string, 0, len(parents))
	for _, p := range parents {
		ids = append(ids, p.UnitID)
	}
	return ids
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
	// No linked leaf: still check this product. AssignOwned and
	// ListOwnedParents must not skip IsEligible after RequiresLinkedParent
	// is cleared. New admin rows are refused earlier by requireChildLinkedParent.
	if len(ids) == 0 {
		return []string{productID}, nil
	}
	return ids, nil
}

func (f *Flow) parentEligibleForLeaves(ctx context.Context, leafIDs []string, parentProductID string) (bool, error) {
	checked := false
	for _, leafID := range leafIDs {
		leafID = strings.TrimSpace(leafID)
		if leafID == "" {
			continue
		}
		checked = true
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
	if !checked {
		return false, nil
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
		if apperror.Is(err, apperror.CodeValidation) {
			return ownedUnit{}, apperror.Validation(notOwned)
		}
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
		if apperror.Is(err, apperror.CodeValidation) {
			return ownedUnit{}, apperror.Validation(notOwned)
		}
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

func (f *Flow) lookupOrders(ctx context.Context, ids []string) (map[string]*order.Order, error) {
	unique := uniqueNonEmpty(ids)
	if len(unique) == 0 {
		return map[string]*order.Order{}, nil
	}
	batchIDs := make([]string, 0, len(unique))
	for _, orderID := range unique {
		if id.IsUUID(orderID) {
			batchIDs = append(batchIDs, orderID)
		}
	}
	if batch, ok := f.orders.(OrderBatchLookup); ok && len(batchIDs) > 0 {
		found, err := batch.FindByIDs(ctx, batchIDs)
		if err == nil {
			if found == nil {
				return map[string]*order.Order{}, nil
			}
			return found, nil
		}
		if !apperror.Is(err, apperror.CodeValidation) {
			return nil, fmt.Errorf("assignment flow: lookup orders: %w", err)
		}
	}
	out := make(map[string]*order.Order, len(unique))
	for _, orderID := range unique {
		o, err := f.orders.FindByID(ctx, orderID)
		if err != nil {
			if apperror.Is(err, apperror.CodeValidation) {
				continue
			}
			return nil, fmt.Errorf("assignment flow: lookup order: %w", err)
		}
		if o != nil {
			out[orderID] = o
		}
	}
	return out, nil
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
