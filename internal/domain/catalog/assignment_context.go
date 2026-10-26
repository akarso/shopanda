package catalog

import (
	"context"
	"strings"
)

// RequiresLinkedParentNotPurchasableMessage is the cart and checkout
// validation text when a product has RequiresLinkedParent set and the
// request assignment context is empty. It is independent of the visibility
// enforce flag and of a missing visibility snapshot.
const RequiresLinkedParentNotPurchasableMessage = "product requires a linked parent and is not purchasable"

// RequiresLinkedParentCode is the admin ExplainAxes code on the purchasable
// axis when RequiresLinkedParent is set and the assignment context is empty.
// It is not an AutoBasis code and is not part of the shared PLP failure list.
const RequiresLinkedParentCode = "requires_linked_parent"

// AssignmentContext is the purchase-time hint that a RequiresLinkedParent
// product is being bought against a parent (PR-1068). Cart lines persist
// ParentProductID and Deferred so claim, qty increase, and checkout can
// re-read the same context. HTTP writers are PR-1070.
//
// Deferred or any non-whitespace ParentProductID leaves the gate. That
// parent string is not checked for existence, eligibility, or ownership
// until PR-1070. Storefront must not set Deferred on every request or the
// flag is meaningless. IsEligible runs in PR-1070 when a parent is present.
type AssignmentContext struct {
	ParentProductID string
	Deferred        bool
}

// Empty reports whether the context supplies no parent and is not deferred.
// A whitespace-only ParentProductID is empty. A nil-equivalent zero value
// is empty.
func (a AssignmentContext) Empty() bool {
	return !a.Deferred && strings.TrimSpace(a.ParentProductID) == ""
}

type assignmentContextKey struct{}

// WithAssignmentContext stores ac on ctx. A whitespace ParentProductID is
// stored trimmed, so a later AssignmentContextFrom sees the same emptiness
// as Empty. A nil ctx is treated as Background.
func WithAssignmentContext(ctx context.Context, ac AssignmentContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ac.ParentProductID = strings.TrimSpace(ac.ParentProductID)
	return context.WithValue(ctx, assignmentContextKey{}, ac)
}

// AssignmentContextFrom returns the assignment context stored on ctx.
// A missing value, a nil ctx, or a whitespace-only parent (with Deferred
// false) is empty.
func AssignmentContextFrom(ctx context.Context) AssignmentContext {
	if ctx == nil {
		return AssignmentContext{}
	}
	ac, ok := ctx.Value(assignmentContextKey{}).(AssignmentContext)
	if !ok {
		return AssignmentContext{}
	}
	ac.ParentProductID = strings.TrimSpace(ac.ParentProductID)
	return ac
}

// ResolvePurchaseAssignment prefers a stored cart-line context over the
// request context. An empty line falls back to AssignmentContextFrom(ctx).
func ResolvePurchaseAssignment(ctx context.Context, line AssignmentContext) AssignmentContext {
	if !line.Empty() {
		return line
	}
	return AssignmentContextFrom(ctx)
}

type purchaseAssignmentsKey struct{}

// WithPurchaseAssignments stores one resolved assignment per variant id.
// PurchaseAssignmentForVariant returns only that variant's entry, so a
// pricing pass can price each line without applying one line's parent to
// the next. A nil ctx is treated as Background. Parent ids are stored trimmed.
func WithPurchaseAssignments(ctx context.Context, byVariant map[string]AssignmentContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	copied := make(map[string]AssignmentContext, len(byVariant))
	for id, ac := range byVariant {
		ac.ParentProductID = strings.TrimSpace(ac.ParentProductID)
		copied[id] = ac
	}
	return context.WithValue(ctx, purchaseAssignmentsKey{}, copied)
}

// PurchaseAssignmentForVariant returns the assignment stored for variantID.
// A missing map, a missing id, or a nil ctx reports ok false. It does not
// fall back to another variant or to AssignmentContextFrom.
func PurchaseAssignmentForVariant(ctx context.Context, variantID string) (AssignmentContext, bool) {
	if ctx == nil || variantID == "" {
		return AssignmentContext{}, false
	}
	byVariant, ok := ctx.Value(purchaseAssignmentsKey{}).(map[string]AssignmentContext)
	if !ok {
		return AssignmentContext{}, false
	}
	ac, ok := byVariant[variantID]
	return ac, ok
}

// LinkedParentBlocksPurchase is the purchasable-axis override. A nil
// product is not blocked here (callers already reject missing products).
func LinkedParentBlocksPurchase(p *Product, assignment AssignmentContext) bool {
	return p != nil && p.RequiresLinkedParent && assignment.Empty()
}
