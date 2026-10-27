package assignment_test

import (
	"context"
	"strings"
	"testing"

	app "github.com/akarso/shopanda/internal/application/assignment"
	"github.com/akarso/shopanda/internal/domain/assignment"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

type stubEligibility struct {
	ok    map[string]bool
	calls int
}

func (s *stubEligibility) IsEligible(_ context.Context, childID, parentID string) (bool, error) {
	s.calls++
	if s.ok == nil {
		return false, nil
	}
	return s.ok[childID+"|"+parentID], nil
}

type stubOrders struct {
	byID       map[string]*order.Order
	byCustomer map[string][]order.Order
	calls      int
	batchCalls int
	err        error
}

func (s *stubOrders) FindByID(_ context.Context, id string) (*order.Order, error) {
	if s == nil {
		return nil, nil
	}
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	if s.byID == nil {
		return nil, nil
	}
	return s.byID[id], nil
}

func (s *stubOrders) FindByCustomerID(_ context.Context, customerID string) ([]order.Order, error) {
	if s.byCustomer == nil {
		return nil, nil
	}
	return s.byCustomer[customerID], nil
}

func (s *stubOrders) FindByIDs(_ context.Context, ids []string) (map[string]*order.Order, error) {
	s.batchCalls++
	if s.err != nil {
		return nil, s.err
	}
	out := make(map[string]*order.Order, len(ids))
	for _, id := range ids {
		if s.byID == nil {
			continue
		}
		if o := s.byID[id]; o != nil {
			out[id] = o
		}
	}
	return out, nil
}

type stubVariants struct {
	byID       map[string]*catalog.Variant
	idCalls    int
	batchCalls int
	err        error
}

func (s *stubVariants) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	s.idCalls++
	if s.err != nil {
		return nil, s.err
	}
	if s.byID == nil {
		return nil, nil
	}
	return s.byID[id], nil
}

func (s *stubVariants) FindByIDs(_ context.Context, ids []string) (map[string]*catalog.Variant, error) {
	s.batchCalls++
	out := make(map[string]*catalog.Variant, len(ids))
	for _, id := range ids {
		if s.byID == nil {
			continue
		}
		if v := s.byID[id]; v != nil {
			out[id] = v
		}
	}
	return out, nil
}

type stubPayments struct {
	byOrder    map[string]*payment.Payment
	idCalls    int
	batchCalls int
}

func (s *stubPayments) FindByOrderID(_ context.Context, orderID string) (*payment.Payment, error) {
	s.idCalls++
	if s == nil || s.byOrder == nil {
		return nil, nil
	}
	return s.byOrder[orderID], nil
}

func (s *stubPayments) FindByOrderIDs(_ context.Context, orderIDs []string) (map[string]*payment.Payment, error) {
	s.batchCalls++
	out := make(map[string]*payment.Payment, len(orderIDs))
	for _, id := range orderIDs {
		if s.byOrder == nil {
			continue
		}
		if py := s.byOrder[id]; py != nil {
			out[id] = py
		}
	}
	return out, nil
}

func mustOrder(t *testing.T, customerID, variantID string) *order.Order {
	t.Helper()
	item, err := order.NewItem(variantID, "SKU-1", "Card", 1, shared.MustNewMoney(0, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(id.New(), customerID, "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	return &o
}

func adminBind(flow *app.Flow, child, parent string) (*app.AdminAssignResult, error) {
	return flow.AdminAssign(context.Background(), app.AdminAssignRequest{
		ChildUnitID:  child,
		ParentUnitID: parent,
	})
}

func linkChild(flow *app.Flow, productID string) *app.Flow {
	return flow.WithLinkedLeaves(stubProducts{byID: map[string]*catalog.Product{
		productID: {ID: productID, Type: catalog.TypeSimple, Status: catalog.StatusActive, RequiresLinkedParent: true},
	}}, stubComponents{})
}

func newFlow(t *testing.T, repo *stubRepo, elig *stubEligibility, orders *stubOrders, variants *stubVariants) *app.Flow {
	t.Helper()
	return newFlowWithPayments(t, repo, elig, orders, variants, nil)
}

func newFlowWithPayments(t *testing.T, repo *stubRepo, elig *stubEligibility, orders *stubOrders, variants *stubVariants, payments *stubPayments) *app.Flow {
	t.Helper()
	if repo == nil {
		repo = &stubRepo{}
	}
	if elig == nil {
		elig = &stubEligibility{}
	}
	if orders == nil {
		orders = &stubOrders{}
	}
	if variants == nil {
		variants = &stubVariants{}
	}
	if payments == nil {
		payments = &stubPayments{}
	}
	return app.NewFlow(app.NewService(repo), repo, elig, orders, variants, payments)
}

func mustPaidOrder(t *testing.T, customerID, variantID string, amount int64) (*order.Order, *payment.Payment) {
	t.Helper()
	item, err := order.NewItem(variantID, "SKU-1", "Card", 1, shared.MustNewMoney(amount, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(id.New(), customerID, "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	if amount == 0 {
		return &o, nil
	}
	py, err := payment.NewPayment(id.New(), o.ID, payment.MethodManual, shared.MustNewMoney(amount, "EUR"))
	if err != nil {
		t.Fatalf("NewPayment: %v", err)
	}
	if err := py.Complete("manual:ok"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	return &o, &py
}

func TestFlow_RecordPurchase_UnassignedWhenDeferred(t *testing.T) {
	repo := &stubRepo{}
	flow := newFlow(t, repo, &stubEligibility{ok: map[string]bool{}}, nil, nil)
	orderID := id.New()
	err := flow.RecordPurchase(context.Background(), "cust-1", orderID, []app.PurchaseLine{{
		ChildVariantID: "ticket-var",
		ChildProductID: "ticket",
		Assignment:     catalog.AssignmentContext{Deferred: true},
	}})
	if err != nil {
		t.Fatalf("RecordPurchase: %v", err)
	}
	if len(repo.inserts) != 1 || repo.inserts[0].Status() != assignment.StatusUnassigned {
		t.Fatalf("inserts = %+v", repo.inserts)
	}
	if repo.inserts[0].ChildOrderItemID != assignment.UnitID(orderID, "ticket-var") {
		t.Fatalf("child = %q", repo.inserts[0].ChildOrderItemID)
	}
}

func TestFlow_RecordPurchase_AssignedWhenParentUnitEligible(t *testing.T) {
	parentOrder := mustOrder(t, "cust-1", "card-var")
	parentUnit := assignment.UnitID(parentOrder.ID, "card-var")
	repo := &stubRepo{}
	elig := &stubEligibility{ok: map[string]bool{"ticket|card-prod": true}}
	orders := &stubOrders{byID: map[string]*order.Order{parentOrder.ID: parentOrder}}
	variants := &stubVariants{byID: map[string]*catalog.Variant{
		"card-var": {ID: "card-var", ProductID: "card-prod"},
	}}
	flow := newFlow(t, repo, elig, orders, variants)

	orderID := id.New()
	err := flow.RecordPurchase(context.Background(), "cust-1", orderID, []app.PurchaseLine{{
		ChildVariantID: "ticket-var",
		ChildProductID: "ticket",
		Assignment:     catalog.AssignmentContext{ParentUnitID: parentUnit, ParentProductID: "card-prod"},
	}})
	if err != nil {
		t.Fatalf("RecordPurchase: %v", err)
	}
	if len(repo.inserts) != 1 || repo.inserts[0].Status() != assignment.StatusAssigned {
		t.Fatalf("inserts = %+v", repo.inserts)
	}
	if repo.inserts[0].ParentOrderItemID != parentUnit {
		t.Fatalf("parent = %q", repo.inserts[0].ParentOrderItemID)
	}
	if elig.calls == 0 {
		t.Fatal("expected IsEligible")
	}
}

func TestFlow_CheckPurchase_RejectsIneligibleParent(t *testing.T) {
	flow := newFlow(t, nil, &stubEligibility{ok: map[string]bool{}}, nil, nil)
	err := flow.CheckPurchase(context.Background(), "cust-1", "ticket", catalog.AssignmentContext{ParentProductID: "card-prod"})
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), app.ParentNotEligibleMessage) {
		t.Fatalf("err = %v, want %q", err, app.ParentNotEligibleMessage)
	}
}

func TestFlow_CheckPurchase_GuestCannotUseParentUnit(t *testing.T) {
	flow := newFlow(t, nil, &stubEligibility{ok: map[string]bool{"ticket|card": true}}, nil, nil)
	err := flow.CheckPurchase(context.Background(), "", "ticket", catalog.AssignmentContext{ParentUnitID: "order/var"})
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), app.ParentUnitSignInMessage) {
		t.Fatalf("err = %v, want sign-in", err)
	}
}

func TestFlow_CheckPurchase_DeferredSkipsEligibility(t *testing.T) {
	elig := &stubEligibility{ok: map[string]bool{}}
	flow := newFlow(t, nil, elig, nil, nil)
	if err := flow.CheckPurchase(context.Background(), "cust-1", "ticket", catalog.AssignmentContext{Deferred: true}); err != nil {
		t.Fatalf("deferred: %v", err)
	}
	if elig.calls != 0 {
		t.Fatalf("IsEligible calls = %d", elig.calls)
	}
}

func TestFlow_AssignOwned_UnassignedThenAssign(t *testing.T) {
	parentOrder := mustOrder(t, "cust-1", "card-var")
	childOrder := mustOrder(t, "cust-1", "ticket-var")
	parentUnit := assignment.UnitID(parentOrder.ID, "card-var")
	childUnit := assignment.UnitID(childOrder.ID, "ticket-var")
	repo := &stubRepo{}
	svc := app.NewService(repo)
	if _, err := svc.CreateUnassigned(context.Background(), childUnit); err != nil {
		t.Fatalf("CreateUnassigned: %v", err)
	}
	elig := &stubEligibility{ok: map[string]bool{"ticket-prod|card-prod": true}}
	orders := &stubOrders{byID: map[string]*order.Order{
		parentOrder.ID: parentOrder,
		childOrder.ID:  childOrder,
	}}
	variants := &stubVariants{byID: map[string]*catalog.Variant{
		"card-var":   {ID: "card-var", ProductID: "card-prod"},
		"ticket-var": {ID: "ticket-var", ProductID: "ticket-prod"},
	}}
	flow := app.NewFlow(svc, repo, elig, orders, variants, &stubPayments{})

	got, err := flow.AssignOwned(context.Background(), "cust-1", childUnit, parentUnit)
	if err != nil {
		t.Fatalf("AssignOwned: %v", err)
	}
	if got.Status() != assignment.StatusAssigned || got.ParentOrderItemID != parentUnit {
		t.Fatalf("got %+v", got)
	}
}

func TestFlow_AssignOwned_CreatesAssignedWhenNoCurrentRow(t *testing.T) {
	parentOrder := mustOrder(t, "cust-1", "card-var")
	childOrder := mustOrder(t, "cust-1", "ticket-var")
	parentUnit := assignment.UnitID(parentOrder.ID, "card-var")
	childUnit := assignment.UnitID(childOrder.ID, "ticket-var")
	repo := &stubRepo{}
	elig := &stubEligibility{ok: map[string]bool{"ticket-prod|card-prod": true}}
	orders := &stubOrders{byID: map[string]*order.Order{
		parentOrder.ID: parentOrder,
		childOrder.ID:  childOrder,
	}}
	variants := &stubVariants{byID: map[string]*catalog.Variant{
		"card-var":   {ID: "card-var", ProductID: "card-prod"},
		"ticket-var": {ID: "ticket-var", ProductID: "ticket-prod"},
	}}
	flow := newFlow(t, repo, elig, orders, variants)

	got, err := flow.AssignOwned(context.Background(), "cust-1", childUnit, parentUnit)
	if err != nil {
		t.Fatalf("AssignOwned: %v", err)
	}
	if got.Status() != assignment.StatusAssigned || len(repo.inserts) != 1 {
		t.Fatalf("got %+v inserts=%d", got, len(repo.inserts))
	}
}

func TestFlow_AssignOwned_RejectsOtherCustomer(t *testing.T) {
	parentOrder := mustOrder(t, "other", "card-var")
	childOrder := mustOrder(t, "cust-1", "ticket-var")
	flow := newFlow(t, nil, &stubEligibility{ok: map[string]bool{"ticket-prod|card-prod": true}}, &stubOrders{byID: map[string]*order.Order{
		parentOrder.ID: parentOrder,
		childOrder.ID:  childOrder,
	}}, &stubVariants{byID: map[string]*catalog.Variant{
		"card-var":   {ID: "card-var", ProductID: "card-prod"},
		"ticket-var": {ID: "ticket-var", ProductID: "ticket-prod"},
	}})
	_, err := flow.AssignOwned(context.Background(), "cust-1",
		assignment.UnitID(childOrder.ID, "ticket-var"),
		assignment.UnitID(parentOrder.ID, "card-var"))
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), app.ParentNotOwnedMessage) {
		t.Fatalf("err = %v, want not owned", err)
	}
}

func TestFlow_ListOwnedParents_FiltersIneligibleAndCancelled(t *testing.T) {
	okOrder := mustOrder(t, "cust-1", "card-var")
	otherItem, err := order.NewItem("other-var", "SKU-2", "Other", 1, shared.MustNewMoney(100, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	mixed, err := order.NewOrder(id.New(), "cust-1", "", "EUR", []order.Item{otherItem})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	cancelled := mustOrder(t, "cust-1", "card-var")
	if err := cancelled.SetStatusFromDB(string(order.OrderStatusCancelled)); err != nil {
		t.Fatalf("SetStatusFromDB: %v", err)
	}
	elig := &stubEligibility{ok: map[string]bool{"ticket|card-prod": true}}
	flow := newFlow(t, nil, elig, &stubOrders{
		byCustomer: map[string][]order.Order{"cust-1": {*okOrder, mixed, *cancelled}},
	}, &stubVariants{byID: map[string]*catalog.Variant{
		"card-var":  {ID: "card-var", ProductID: "card-prod"},
		"other-var": {ID: "other-var", ProductID: "other-prod"},
	}})
	got, err := flow.ListOwnedParents(context.Background(), "cust-1", "ticket")
	if err != nil {
		t.Fatalf("ListOwnedParents: %v", err)
	}
	if len(got) != 1 || got[0].UnitID != assignment.UnitID(okOrder.ID, "card-var") {
		t.Fatalf("got %+v", got)
	}
}

func TestFlow_ListOwnedParents_SkipsUnpaidPendingAndFailed(t *testing.T) {
	zero := mustOrder(t, "cust-1", "card-var")
	paid, paidPy := mustPaidOrder(t, "cust-1", "card-var", 1000)
	unpaid, _ := mustPaidOrder(t, "cust-1", "card-var", 1000)
	pendingOrder, _ := mustPaidOrder(t, "cust-1", "card-var", 1000)
	pendingPy, err := payment.NewPayment(id.New(), pendingOrder.ID, payment.MethodManual, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("NewPayment pending: %v", err)
	}
	failedOrder, _ := mustPaidOrder(t, "cust-1", "card-var", 1000)
	failedPy, err := payment.NewPayment(id.New(), failedOrder.ID, payment.MethodManual, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("NewPayment failed: %v", err)
	}
	if err := failedPy.Fail(); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	elig := &stubEligibility{ok: map[string]bool{"ticket|card-prod": true}}
	variants := &stubVariants{byID: map[string]*catalog.Variant{
		"card-var": {ID: "card-var", ProductID: "card-prod"},
	}}
	payments := &stubPayments{byOrder: map[string]*payment.Payment{
		paid.ID:         paidPy,
		pendingOrder.ID: &pendingPy,
		failedOrder.ID:  &failedPy,
	}}
	flow := newFlowWithPayments(t, nil, elig, &stubOrders{
		byCustomer: map[string][]order.Order{"cust-1": {*zero, *paid, *unpaid, *pendingOrder, *failedOrder}},
	}, variants, payments)
	got, err := flow.ListOwnedParents(context.Background(), "cust-1", "ticket")
	if err != nil {
		t.Fatalf("ListOwnedParents: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v, want zero-payable and paid", got)
	}
	want := map[string]bool{
		assignment.UnitID(zero.ID, "card-var"): true,
		assignment.UnitID(paid.ID, "card-var"): true,
	}
	for _, parent := range got {
		if !want[parent.UnitID] {
			t.Fatalf("unexpected parent %q", parent.UnitID)
		}
	}
	if variants.batchCalls != 1 {
		t.Fatalf("FindByIDs calls = %d, want 1", variants.batchCalls)
	}
	if variants.idCalls != 0 {
		t.Fatalf("FindByID calls = %d, want 0", variants.idCalls)
	}
	if elig.calls != 1 {
		t.Fatalf("IsEligible calls = %d, want 1", elig.calls)
	}
	if payments.batchCalls != 1 {
		t.Fatalf("FindByOrderIDs calls = %d, want 1", payments.batchCalls)
	}
	if payments.idCalls != 0 {
		t.Fatalf("FindByOrderID calls = %d, want 0", payments.idCalls)
	}
}

func TestFlow_AssignOwned_RejectsUnpaidParent(t *testing.T) {
	parent, _ := mustPaidOrder(t, "cust-1", "card-var", 1000)
	child := mustOrder(t, "cust-1", "ticket-var")
	flow := newFlow(t, nil, &stubEligibility{ok: map[string]bool{"ticket-prod|card-prod": true}}, &stubOrders{byID: map[string]*order.Order{
		parent.ID: parent,
		child.ID:  child,
	}}, &stubVariants{byID: map[string]*catalog.Variant{
		"card-var":   {ID: "card-var", ProductID: "card-prod"},
		"ticket-var": {ID: "ticket-var", ProductID: "ticket-prod"},
	}})
	_, err := flow.AssignOwned(context.Background(), "cust-1",
		assignment.UnitID(child.ID, "ticket-var"),
		assignment.UnitID(parent.ID, "card-var"))
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), app.ParentNotOwnedMessage) {
		t.Fatalf("err = %v, want not owned", err)
	}
}

func TestFlow_RevokeForOrder_ClearsCurrent(t *testing.T) {
	o := mustOrder(t, "cust-1", "ticket-var")
	repo := &stubRepo{}
	flow := newFlow(t, repo, nil, &stubOrders{byID: map[string]*order.Order{o.ID: o}}, nil)
	if err := flow.RecordPurchase(context.Background(), "cust-1", o.ID, []app.PurchaseLine{{
		ChildVariantID: "ticket-var",
		ChildProductID: "ticket",
		Assignment:     catalog.AssignmentContext{Deferred: true},
	}}); err != nil {
		t.Fatalf("RecordPurchase: %v", err)
	}
	if err := flow.RevokeForOrder(context.Background(), o.ID); err != nil {
		t.Fatalf("RevokeForOrder: %v", err)
	}
	current, err := app.NewService(repo).CurrentByChild(context.Background(), assignment.UnitID(o.ID, "ticket-var"))
	if err != nil {
		t.Fatalf("CurrentByChild: %v", err)
	}
	if current != nil {
		t.Fatalf("current = %+v, want nil after revoke", current)
	}
}

func TestFlow_AssertParentReturnable_BlocksAssignedChildren(t *testing.T) {
	parent := mustOrder(t, "cust-1", "card-var")
	child := mustOrder(t, "cust-1", "ticket-var")
	parentUnit := assignment.UnitID(parent.ID, "card-var")
	childUnit := assignment.UnitID(child.ID, "ticket-var")
	repo := &stubRepo{}
	svc := app.NewService(repo)
	if _, err := svc.CreateAssigned(context.Background(), parentUnit, childUnit); err != nil {
		t.Fatalf("CreateAssigned: %v", err)
	}
	flow := newFlow(t, repo, nil, &stubOrders{}, nil)

	err := flow.AssertParentReturnable(context.Background(), parentUnit)
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), app.ParentHasAssignedChildrenMessage) {
		t.Fatalf("err = %v, want parent-has-children", err)
	}

	if _, _, err := svc.Revoke(context.Background(), repo.inserts[0].ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := flow.AssertParentReturnable(context.Background(), parentUnit); err != nil {
		t.Fatalf("after revoke: %v", err)
	}
}

func TestFlow_AssertParentReturnable_SkipsChildrenOnSameRequest(t *testing.T) {
	parent := mustOrder(t, "cust-1", "card-var")
	child := mustOrder(t, "cust-1", "ticket-var")
	parentUnit := assignment.UnitID(parent.ID, "card-var")
	childUnit := assignment.UnitID(child.ID, "ticket-var")
	repo := &stubRepo{}
	svc := app.NewService(repo)
	if _, err := svc.CreateAssigned(context.Background(), parentUnit, childUnit); err != nil {
		t.Fatalf("CreateAssigned: %v", err)
	}
	flow := newFlow(t, repo, nil, &stubOrders{}, nil)

	if err := flow.AssertParentReturnable(context.Background(), parentUnit, childUnit); err != nil {
		t.Fatalf("skip child on request: %v", err)
	}
	err := flow.AssertParentReturnable(context.Background(), parentUnit)
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("without skip = %v, want validation", err)
	}
}

func TestFlow_RevokeForUnit_AssignedAndUnassigned(t *testing.T) {
	parent := mustOrder(t, "cust-1", "card-var")
	child := mustOrder(t, "cust-1", "ticket-var")
	parentUnit := assignment.UnitID(parent.ID, "card-var")
	childUnit := assignment.UnitID(child.ID, "ticket-var")
	repo := &stubRepo{}
	svc := app.NewService(repo)
	flow := newFlow(t, repo, nil, &stubOrders{}, nil)

	if _, err := flow.RevokeForUnit(context.Background(), childUnit, ""); err != nil {
		t.Fatalf("missing row: %v", err)
	}

	if _, err := svc.CreateAssigned(context.Background(), parentUnit, childUnit); err != nil {
		t.Fatalf("CreateAssigned: %v", err)
	}
	if _, err := flow.RevokeForUnit(context.Background(), childUnit, ""); err != nil {
		t.Fatalf("RevokeForUnit assigned: %v", err)
	}
	current, err := svc.CurrentByChild(context.Background(), childUnit)
	if err != nil || current != nil {
		t.Fatalf("current after assigned revoke = %+v %v", current, err)
	}
	if _, err := flow.RevokeForUnit(context.Background(), childUnit, ""); err != nil {
		t.Fatalf("idempotent revoke: %v", err)
	}

	unassignedChild := mustOrder(t, "cust-1", "ticket-2")
	unassignedUnit := assignment.UnitID(unassignedChild.ID, "ticket-2")
	if _, err := svc.CreateUnassigned(context.Background(), unassignedUnit); err != nil {
		t.Fatalf("CreateUnassigned: %v", err)
	}
	if _, err := flow.RevokeForUnit(context.Background(), unassignedUnit, ""); err != nil {
		t.Fatalf("RevokeForUnit unassigned: %v", err)
	}
	current, err = svc.CurrentByChild(context.Background(), unassignedUnit)
	if err != nil || current != nil {
		t.Fatalf("current after unassigned revoke = %+v %v", current, err)
	}
}

type stubProducts struct {
	byID map[string]*catalog.Product
}

func (s stubProducts) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	if s.byID == nil {
		return nil, nil
	}
	return s.byID[id], nil
}

type stubComponents struct {
	byBundle map[string][]catalog.BundleComponent
}

func (s stubComponents) ListByBundleID(_ context.Context, bundleID string) ([]catalog.BundleComponent, error) {
	if s.byBundle == nil {
		return nil, nil
	}
	return s.byBundle[bundleID], nil
}

func TestFlow_AssignOwned_UsesLinkedLeavesForBundle(t *testing.T) {
	parentOrder := mustOrder(t, "cust-1", "card-var")
	childOrder := mustOrder(t, "cust-1", "bundle-var")
	parentUnit := assignment.UnitID(parentOrder.ID, "card-var")
	childUnit := assignment.UnitID(childOrder.ID, "bundle-var")
	repo := &stubRepo{}
	elig := &stubEligibility{ok: map[string]bool{"ticket-leaf|card-prod": true}}
	orders := &stubOrders{byID: map[string]*order.Order{
		parentOrder.ID: parentOrder,
		childOrder.ID:  childOrder,
	}}
	variants := &stubVariants{byID: map[string]*catalog.Variant{
		"card-var":   {ID: "card-var", ProductID: "card-prod"},
		"bundle-var": {ID: "bundle-var", ProductID: "bundle-prod"},
	}}
	leaf, err := catalog.NewBundleComponent("bundle-prod", "ticket-leaf", 1)
	if err != nil {
		t.Fatalf("NewBundleComponent: %v", err)
	}
	flow := newFlow(t, repo, elig, orders, variants).WithLinkedLeaves(
		stubProducts{byID: map[string]*catalog.Product{
			"bundle-prod": {ID: "bundle-prod", Type: catalog.TypeBundle, Status: catalog.StatusActive},
			"ticket-leaf": {ID: "ticket-leaf", Type: catalog.TypeSimple, Status: catalog.StatusActive, RequiresLinkedParent: true},
		}},
		stubComponents{byBundle: map[string][]catalog.BundleComponent{"bundle-prod": {*leaf}}},
	)
	got, err := flow.AssignOwned(context.Background(), "cust-1", childUnit, parentUnit)
	if err != nil {
		t.Fatalf("AssignOwned: %v", err)
	}
	if got.ParentOrderItemID != parentUnit {
		t.Fatalf("parent = %q", got.ParentOrderItemID)
	}
}

func TestFlow_ListOwnedParents_UsesLinkedLeavesForBundle(t *testing.T) {
	parentOrder := mustOrder(t, "cust-1", "card-var")
	elig := &stubEligibility{ok: map[string]bool{"ticket-leaf|card-prod": true}}
	orders := &stubOrders{
		byID:       map[string]*order.Order{parentOrder.ID: parentOrder},
		byCustomer: map[string][]order.Order{"cust-1": {*parentOrder}},
	}
	variants := &stubVariants{byID: map[string]*catalog.Variant{
		"card-var": {ID: "card-var", ProductID: "card-prod"},
	}}
	leaf, err := catalog.NewBundleComponent("bundle-prod", "ticket-leaf", 1)
	if err != nil {
		t.Fatalf("NewBundleComponent: %v", err)
	}
	flow := newFlow(t, nil, elig, orders, variants).WithLinkedLeaves(
		stubProducts{byID: map[string]*catalog.Product{
			"bundle-prod": {ID: "bundle-prod", Type: catalog.TypeBundle, Status: catalog.StatusActive},
			"ticket-leaf": {ID: "ticket-leaf", Type: catalog.TypeSimple, Status: catalog.StatusActive, RequiresLinkedParent: true},
		}},
		stubComponents{byBundle: map[string][]catalog.BundleComponent{"bundle-prod": {*leaf}}},
	)
	got, err := flow.ListOwnedParents(context.Background(), "cust-1", "bundle-prod")
	if err != nil {
		t.Fatalf("ListOwnedParents: %v", err)
	}
	if len(got) != 1 || got[0].ProductID != "card-prod" {
		t.Fatalf("parents = %+v, want eligible card for bundle leaves", got)
	}
}

func TestFlow_AssignOwned_ChecksEligibilityWhenLinkedParentCleared(t *testing.T) {
	parentOrder := mustOrder(t, "cust-1", "card-var")
	childOrder := mustOrder(t, "cust-1", "ticket-var")
	parentUnit := assignment.UnitID(parentOrder.ID, "card-var")
	childUnit := assignment.UnitID(childOrder.ID, "ticket-var")
	orders := &stubOrders{byID: map[string]*order.Order{
		parentOrder.ID: parentOrder,
		childOrder.ID:  childOrder,
	}}
	variants := &stubVariants{byID: map[string]*catalog.Variant{
		"card-var":   {ID: "card-var", ProductID: "card-prod"},
		"ticket-var": {ID: "ticket-var", ProductID: "ticket-prod"},
	}}
	products := stubProducts{byID: map[string]*catalog.Product{
		"ticket-prod": {ID: "ticket-prod", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}

	repo := &stubRepo{}
	if _, err := app.NewService(repo).CreateUnassigned(context.Background(), childUnit); err != nil {
		t.Fatalf("CreateUnassigned: %v", err)
	}
	ineligible := &stubEligibility{}
	flow := newFlow(t, repo, ineligible, orders, variants).WithLinkedLeaves(products, stubComponents{})
	_, err := flow.AssignOwned(context.Background(), "cust-1", childUnit, parentUnit)
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), app.ParentNotEligibleMessage) {
		t.Fatalf("ineligible parent = %v", err)
	}
	if ineligible.calls == 0 {
		t.Fatal("cleared RequiresLinkedParent skipped IsEligible")
	}
	if len(repo.updates) != 0 {
		t.Fatalf("updates = %d, want none", len(repo.updates))
	}

	eligible := &stubEligibility{ok: map[string]bool{"ticket-prod|card-prod": true}}
	flow = newFlow(t, repo, eligible, orders, variants).WithLinkedLeaves(products, stubComponents{})
	got, err := flow.AssignOwned(context.Background(), "cust-1", childUnit, parentUnit)
	if err != nil {
		t.Fatalf("eligible parent: %v", err)
	}
	if got.Status() != assignment.StatusAssigned || got.ParentOrderItemID != parentUnit {
		t.Fatalf("got %+v", got)
	}
	if eligible.calls == 0 {
		t.Fatal("eligible bind did not check IsEligible")
	}
}

func TestFlow_ListOwnedParents_WhenLinkedParentCleared(t *testing.T) {
	parentOrder := mustOrder(t, "cust-1", "card-var")
	elig := &stubEligibility{ok: map[string]bool{"ticket-prod|card-prod": true}}
	flow := newFlow(t, nil, elig, &stubOrders{
		byID:       map[string]*order.Order{parentOrder.ID: parentOrder},
		byCustomer: map[string][]order.Order{"cust-1": {*parentOrder}},
	}, &stubVariants{byID: map[string]*catalog.Variant{
		"card-var": {ID: "card-var", ProductID: "card-prod"},
	}}).WithLinkedLeaves(stubProducts{byID: map[string]*catalog.Product{
		"ticket-prod": {ID: "ticket-prod", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}, stubComponents{})
	got, err := flow.ListOwnedParents(context.Background(), "cust-1", "ticket-prod")
	if err != nil {
		t.Fatalf("ListOwnedParents: %v", err)
	}
	if len(got) != 1 || got[0].UnitID != assignment.UnitID(parentOrder.ID, "card-var") {
		t.Fatalf("parents = %+v", got)
	}
	if elig.calls == 0 {
		t.Fatal("cleared RequiresLinkedParent skipped IsEligible")
	}
}

func TestFlow_AssignOwned_CanonicalizesPaddedUnitIDs(t *testing.T) {
	parentOrder := mustOrder(t, "cust-1", "card-var")
	childOrder := mustOrder(t, "cust-1", "ticket-var")
	parentUnit := assignment.UnitID(parentOrder.ID, "card-var")
	childUnit := assignment.UnitID(childOrder.ID, "ticket-var")
	repo := &stubRepo{}
	svc := app.NewService(repo)
	if _, err := svc.CreateUnassigned(context.Background(), childUnit); err != nil {
		t.Fatalf("CreateUnassigned: %v", err)
	}
	flow := newFlow(t, repo, &stubEligibility{ok: map[string]bool{"ticket-prod|card-prod": true}}, &stubOrders{byID: map[string]*order.Order{
		parentOrder.ID: parentOrder,
		childOrder.ID:  childOrder,
	}}, &stubVariants{byID: map[string]*catalog.Variant{
		"card-var":   {ID: "card-var", ProductID: "card-prod"},
		"ticket-var": {ID: "ticket-var", ProductID: "ticket-prod"},
	}})
	got, err := flow.AssignOwned(context.Background(), "cust-1", " "+childUnit+" ", " "+parentUnit+" ")
	if err != nil {
		t.Fatalf("AssignOwned: %v", err)
	}
	if got.ChildOrderItemID != childUnit || got.ParentOrderItemID != parentUnit {
		t.Fatalf("got child=%q parent=%q, want minted ids", got.ChildOrderItemID, got.ParentOrderItemID)
	}
	current, err := svc.CurrentByChild(context.Background(), childUnit)
	if err != nil || current == nil || current.ID != got.ID {
		t.Fatalf("CurrentByChild = %+v err=%v, want the same row", current, err)
	}
}

func TestFlow_AdminAssign_UnassignedThenAssign(t *testing.T) {
	parentOrder := mustOrder(t, "cust-1", "card-var")
	childOrder := mustOrder(t, "cust-1", "ticket-var")
	parentUnit := assignment.UnitID(parentOrder.ID, "card-var")
	childUnit := assignment.UnitID(childOrder.ID, "ticket-var")
	repo := &stubRepo{}
	svc := app.NewService(repo)
	if _, err := svc.CreateUnassigned(context.Background(), childUnit); err != nil {
		t.Fatalf("CreateUnassigned: %v", err)
	}
	flow := newFlow(t, repo, &stubEligibility{ok: map[string]bool{"ticket-prod|card-prod": true}}, &stubOrders{byID: map[string]*order.Order{
		parentOrder.ID: parentOrder,
		childOrder.ID:  childOrder,
	}}, &stubVariants{byID: map[string]*catalog.Variant{
		"card-var":   {ID: "card-var", ProductID: "card-prod"},
		"ticket-var": {ID: "ticket-var", ProductID: "ticket-prod"},
	}})
	got, err := adminBind(flow, " "+childUnit+" ", " "+parentUnit+" ")
	if err != nil {
		t.Fatalf("AdminAssign: %v", err)
	}
	if got.Created || got.Assignment.Status() != assignment.StatusAssigned || got.Assignment.ParentOrderItemID != parentUnit || got.Assignment.ChildOrderItemID != childUnit {
		t.Fatalf("got %+v", got)
	}
	if len(repo.inserts) != 1 {
		t.Fatalf("inserts = %d, want the unassigned row only", len(repo.inserts))
	}
}

func TestFlow_AdminAssign_CreatesAssignedWhenNoCurrentRow(t *testing.T) {
	parentOrder := mustOrder(t, "cust-1", "card-var")
	childOrder := mustOrder(t, "cust-1", "ticket-var")
	repo := &stubRepo{}
	flow := newFlow(t, repo, &stubEligibility{ok: map[string]bool{"ticket-prod|card-prod": true}}, &stubOrders{byID: map[string]*order.Order{
		parentOrder.ID: parentOrder,
		childOrder.ID:  childOrder,
	}}, &stubVariants{byID: map[string]*catalog.Variant{
		"card-var":   {ID: "card-var", ProductID: "card-prod"},
		"ticket-var": {ID: "ticket-var", ProductID: "ticket-prod"},
	}})
	flow = linkChild(flow, "ticket-prod")
	got, err := adminBind(flow,
		assignment.UnitID(childOrder.ID, "ticket-var"),
		assignment.UnitID(parentOrder.ID, "card-var"))
	if err != nil {
		t.Fatalf("AdminAssign: %v", err)
	}
	if !got.Created || got.Assignment.Status() != assignment.StatusAssigned || len(repo.inserts) != 1 {
		t.Fatalf("got %+v inserts=%d", got, len(repo.inserts))
	}
	if got.Child.CustomerID != "cust-1" || got.Child.OrderStatus != string(order.OrderStatusPending) {
		t.Fatalf("child facts = %+v", got.Child)
	}
}

func TestFlow_AdminAssign_RejectsLiveAssigned(t *testing.T) {
	parentOrder := mustOrder(t, "cust-1", "card-var")
	childOrder := mustOrder(t, "cust-1", "ticket-var")
	parentUnit := assignment.UnitID(parentOrder.ID, "card-var")
	childUnit := assignment.UnitID(childOrder.ID, "ticket-var")
	repo := &stubRepo{}
	svc := app.NewService(repo)
	if _, err := svc.CreateAssigned(context.Background(), parentUnit, childUnit); err != nil {
		t.Fatalf("CreateAssigned: %v", err)
	}
	flow := newFlow(t, repo, &stubEligibility{ok: map[string]bool{"ticket-prod|card-prod": true}}, &stubOrders{byID: map[string]*order.Order{
		parentOrder.ID: parentOrder,
		childOrder.ID:  childOrder,
	}}, &stubVariants{byID: map[string]*catalog.Variant{
		"card-var":   {ID: "card-var", ProductID: "card-prod"},
		"ticket-var": {ID: "ticket-var", ProductID: "ticket-prod"},
	}})
	_, err := adminBind(flow, childUnit, parentUnit)
	if !apperror.Is(err, apperror.CodeConflict) || !strings.Contains(err.Error(), app.AlreadyAssignedMessage) {
		t.Fatalf("err = %v, want conflict", err)
	}
}

func TestFlow_AdminAssign_SkipsCustomerOwnership(t *testing.T) {
	parent, _ := mustPaidOrder(t, "other-customer", "card-var", 1000)
	child := mustOrder(t, "cust-1", "ticket-var")
	if err := child.SetStatusFromDB(string(order.OrderStatusCancelled)); err != nil {
		t.Fatalf("SetStatusFromDB: %v", err)
	}
	payments := &stubPayments{}
	repo := &stubRepo{}
	flow := newFlowWithPayments(t, repo, &stubEligibility{ok: map[string]bool{"ticket-prod|card-prod": true}}, &stubOrders{byID: map[string]*order.Order{
		parent.ID: parent,
		child.ID:  child,
	}}, &stubVariants{byID: map[string]*catalog.Variant{
		"card-var":   {ID: "card-var", ProductID: "card-prod"},
		"ticket-var": {ID: "ticket-var", ProductID: "ticket-prod"},
	}}, payments)
	flow = linkChild(flow, "ticket-prod")
	childUnit := assignment.UnitID(child.ID, "ticket-var")
	parentUnit := assignment.UnitID(parent.ID, "card-var")
	_, err := adminBind(flow, childUnit, parentUnit)
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), app.ChildOrderNotBindableMessage) {
		t.Fatalf("without force = %v", err)
	}
	got, err := flow.AdminAssign(context.Background(), app.AdminAssignRequest{
		ChildUnitID:  childUnit,
		ParentUnitID: parentUnit,
		Force:        true,
		Reason:       "correct a cancelled ticket",
	})
	if err != nil {
		t.Fatalf("force AdminAssign: %v", err)
	}
	if got.Assignment.Status() != assignment.StatusAssigned || got.Reason != "correct a cancelled ticket" {
		t.Fatalf("got %+v", got)
	}
	if got.Child.CustomerID != "cust-1" || got.Parent.CustomerID != "other-customer" {
		t.Fatalf("facts child=%+v parent=%+v", got.Child, got.Parent)
	}
	if got.Child.OrderStatus != string(order.OrderStatusCancelled) {
		t.Fatalf("child order status = %q", got.Child.OrderStatus)
	}
	if !got.Assignment.Forced() || got.Assignment.ForcedReason != "correct a cancelled ticket" {
		t.Fatalf("forced = at:%v reason:%q", got.Assignment.ForcedAt, got.Assignment.ForcedReason)
	}
	current, err := app.NewService(repo).CurrentByChild(context.Background(), strings.ToUpper(child.ID)+"/ticket-var")
	if err != nil || current == nil || current.ID != got.Assignment.ID || current.ChildOrderItemID != childUnit {
		t.Fatalf("uppercase CurrentByChild = %+v %v, want the lowercase row", current, err)
	}
	if len(repo.inserts) != 1 {
		t.Fatalf("inserts = %d, want one row for the uppercase order uuid", len(repo.inserts))
	}
}

func TestFlow_AdminAssign_UppercaseOrderUUIDHitsExistingRow(t *testing.T) {
	parentOrder := mustOrder(t, "cust-1", "card-var")
	childOrder := mustOrder(t, "cust-1", "ticket-var")
	parentUnit := assignment.UnitID(parentOrder.ID, "card-var")
	childUnit := assignment.UnitID(childOrder.ID, "ticket-var")
	repo := &stubRepo{}
	svc := app.NewService(repo)
	if _, err := svc.CreateUnassigned(context.Background(), childUnit); err != nil {
		t.Fatalf("CreateUnassigned: %v", err)
	}
	flow := newFlow(t, repo, &stubEligibility{ok: map[string]bool{"ticket-prod|card-prod": true}}, &stubOrders{byID: map[string]*order.Order{
		parentOrder.ID: parentOrder,
		childOrder.ID:  childOrder,
	}}, &stubVariants{byID: map[string]*catalog.Variant{
		"card-var":   {ID: "card-var", ProductID: "card-prod"},
		"ticket-var": {ID: "ticket-var", ProductID: "ticket-prod"},
	}})
	upperChild := strings.ToUpper(childOrder.ID) + "/ticket-var"
	upperParent := strings.ToUpper(parentOrder.ID) + "/card-var"
	got, err := adminBind(flow, upperChild, upperParent)
	if err != nil {
		t.Fatalf("AdminAssign uppercase: %v", err)
	}
	if got.Created || got.Assignment.ID == "" || got.Assignment.ChildOrderItemID != childUnit || got.Assignment.ParentOrderItemID != parentUnit {
		t.Fatalf("got %+v, want the existing lowercase row", got)
	}
	if len(repo.inserts) != 1 {
		t.Fatalf("inserts = %d, want the unassigned row only", len(repo.inserts))
	}
	current, err := svc.CurrentByChild(context.Background(), upperChild)
	if err != nil || current == nil || current.ID != got.Assignment.ID {
		t.Fatalf("CurrentByChild uppercase = %+v %v", current, err)
	}
}

func TestFlow_AdminAssign_RejectsIneligibleAndMissing(t *testing.T) {
	parentOrder := mustOrder(t, "cust-1", "card-var")
	childOrder := mustOrder(t, "cust-1", "ticket-var")
	parentUnit := assignment.UnitID(parentOrder.ID, "card-var")
	childUnit := assignment.UnitID(childOrder.ID, "ticket-var")
	flow := newFlow(t, nil, &stubEligibility{ok: map[string]bool{}}, &stubOrders{byID: map[string]*order.Order{
		parentOrder.ID: parentOrder,
		childOrder.ID:  childOrder,
	}}, &stubVariants{byID: map[string]*catalog.Variant{
		"card-var":   {ID: "card-var", ProductID: "card-prod"},
		"ticket-var": {ID: "ticket-var", ProductID: "ticket-prod"},
	}})
	flow = linkChild(flow, "ticket-prod")
	_, err := adminBind(flow, childUnit, parentUnit)
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), app.ParentNotEligibleMessage) {
		t.Fatalf("ineligible = %v", err)
	}
	orders := &stubOrders{byID: map[string]*order.Order{
		parentOrder.ID: parentOrder,
		childOrder.ID:  childOrder,
	}}
	flow = newFlow(t, nil, &stubEligibility{ok: map[string]bool{}}, orders, &stubVariants{byID: map[string]*catalog.Variant{
		"card-var":   {ID: "card-var", ProductID: "card-prod"},
		"ticket-var": {ID: "ticket-var", ProductID: "ticket-prod"},
	}})
	_, err = adminBind(flow, "foo/bar", parentUnit)
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), app.ChildUnitInvalidMessage) {
		t.Fatalf("invalid child = %v", err)
	}
	if orders.calls != 0 {
		t.Fatalf("non-uuid order part looked up the order %d times", orders.calls)
	}
	_, err = adminBind(flow, childUnit, "not-a-unit")
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), app.ParentUnitInvalidMessage) {
		t.Fatalf("invalid parent = %v", err)
	}
	_, err = adminBind(flow, assignment.UnitID(id.New(), "ticket-var"), parentUnit)
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), app.ChildUnitMissingMessage) {
		t.Fatalf("missing child = %v", err)
	}
	_, err = adminBind(flow, childUnit, assignment.UnitID(id.New(), "card-var"))
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), app.ParentUnitMissingMessage) {
		t.Fatalf("missing parent = %v", err)
	}
}

func TestFlow_AdminAssign_UsesLinkedLeavesForBundle(t *testing.T) {
	parentOrder := mustOrder(t, "other", "card-var")
	childOrder := mustOrder(t, "cust-1", "bundle-var")
	parentUnit := assignment.UnitID(parentOrder.ID, "card-var")
	childUnit := assignment.UnitID(childOrder.ID, "bundle-var")
	repo := &stubRepo{}
	elig := &stubEligibility{ok: map[string]bool{"ticket-leaf|card-prod": true}}
	leaf, err := catalog.NewBundleComponent("bundle-prod", "ticket-leaf", 1)
	if err != nil {
		t.Fatalf("NewBundleComponent: %v", err)
	}
	flow := newFlow(t, repo, elig, &stubOrders{byID: map[string]*order.Order{
		parentOrder.ID: parentOrder,
		childOrder.ID:  childOrder,
	}}, &stubVariants{byID: map[string]*catalog.Variant{
		"card-var":   {ID: "card-var", ProductID: "card-prod"},
		"bundle-var": {ID: "bundle-var", ProductID: "bundle-prod"},
	}}).WithLinkedLeaves(
		stubProducts{byID: map[string]*catalog.Product{
			"bundle-prod": {ID: "bundle-prod", Type: catalog.TypeBundle, Status: catalog.StatusActive},
			"ticket-leaf": {ID: "ticket-leaf", Type: catalog.TypeSimple, Status: catalog.StatusActive, RequiresLinkedParent: true},
		}},
		stubComponents{byBundle: map[string][]catalog.BundleComponent{"bundle-prod": {*leaf}}},
	)
	got, err := adminBind(flow, childUnit, parentUnit)
	if err != nil {
		t.Fatalf("AdminAssign: %v", err)
	}
	if got.Assignment.ParentOrderItemID != parentUnit || elig.calls == 0 {
		t.Fatalf("got %+v calls=%d", got, elig.calls)
	}
}

func TestFlow_AdminAssign_MissingVariantAndUUIDSyntax(t *testing.T) {
	parentOrder := mustOrder(t, "cust-1", "card-var")
	childOrder := mustOrder(t, "cust-1", "ticket-var")
	parentUnit := assignment.UnitID(parentOrder.ID, "card-var")
	childUnit := assignment.UnitID(childOrder.ID, "ticket-var")
	orders := &stubOrders{byID: map[string]*order.Order{
		parentOrder.ID: parentOrder,
		childOrder.ID:  childOrder,
	}}
	flow := newFlow(t, nil, &stubEligibility{ok: map[string]bool{"ticket-prod|card-prod": true}}, orders, &stubVariants{byID: map[string]*catalog.Variant{
		"card-var": {ID: "card-var", ProductID: "card-prod"},
	}})
	_, err := adminBind(flow, childUnit, parentUnit)
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), app.ChildVariantMissingMessage) || strings.Contains(err.Error(), app.ChildUnitMissingMessage) {
		t.Fatalf("missing variant = %v", err)
	}

	badOrders := &stubOrders{err: apperror.Validation("invalid order id")}
	flow = newFlow(t, nil, &stubEligibility{}, badOrders, &stubVariants{})
	_, err = adminBind(flow, childUnit, parentUnit)
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), app.ChildUnitMissingMessage) || strings.Contains(err.Error(), "lookup order") {
		t.Fatalf("order validation = %v, want child-unit validation", err)
	}

	flow = newFlow(t, nil, &stubEligibility{}, orders, &stubVariants{err: apperror.Validation("invalid variant id")})
	_, err = adminBind(flow, childUnit, parentUnit)
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), app.ChildVariantMissingMessage) || strings.Contains(err.Error(), "lookup variant") {
		t.Fatalf("variant validation = %v, want missing-variant validation", err)
	}
}

func TestFlow_AdminAssign_RequiresLinkedParentOrCurrentRow(t *testing.T) {
	parentOrder := mustOrder(t, "cust-1", "card-var")
	childOrder := mustOrder(t, "cust-1", "ticket-var")
	parentUnit := assignment.UnitID(parentOrder.ID, "card-var")
	childUnit := assignment.UnitID(childOrder.ID, "ticket-var")
	elig := &stubEligibility{ok: map[string]bool{"ticket-prod|card-prod": true}}
	flow := newFlow(t, nil, elig, &stubOrders{byID: map[string]*order.Order{
		parentOrder.ID: parentOrder,
		childOrder.ID:  childOrder,
	}}, &stubVariants{byID: map[string]*catalog.Variant{
		"card-var":   {ID: "card-var", ProductID: "card-prod"},
		"ticket-var": {ID: "ticket-var", ProductID: "ticket-prod"},
	}}).WithLinkedLeaves(stubProducts{byID: map[string]*catalog.Product{
		"ticket-prod": {ID: "ticket-prod", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}, stubComponents{})
	_, err := adminBind(flow, childUnit, parentUnit)
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), app.ChildNotLinkedMessage) {
		t.Fatalf("not linked = %v", err)
	}
	if elig.calls != 0 {
		t.Fatalf("eligibility calls = %d, want none before the linked-parent gate", elig.calls)
	}

	repo := &stubRepo{}
	if _, err := app.NewService(repo).CreateUnassigned(context.Background(), childUnit); err != nil {
		t.Fatalf("CreateUnassigned: %v", err)
	}
	elig = &stubEligibility{ok: map[string]bool{"ticket-prod|card-prod": true}}
	flow = newFlow(t, repo, elig, &stubOrders{byID: map[string]*order.Order{
		parentOrder.ID: parentOrder,
		childOrder.ID:  childOrder,
	}}, &stubVariants{byID: map[string]*catalog.Variant{
		"card-var":   {ID: "card-var", ProductID: "card-prod"},
		"ticket-var": {ID: "ticket-var", ProductID: "ticket-prod"},
	}}).WithLinkedLeaves(stubProducts{byID: map[string]*catalog.Product{
		"ticket-prod": {ID: "ticket-prod", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}, stubComponents{})
	got, err := adminBind(flow, childUnit, parentUnit)
	if err != nil {
		t.Fatalf("unassigned without flag: %v", err)
	}
	if got.Created || got.Assignment.Status() != assignment.StatusAssigned || got.Assignment.ParentOrderItemID != parentUnit {
		t.Fatalf("unassigned without flag = %+v", got)
	}
	if elig.calls == 0 {
		t.Fatal("unassigned row did not check eligibility for the order-line product")
	}

	repo = &stubRepo{}
	if _, err := app.NewService(repo).CreateUnassigned(context.Background(), childUnit); err != nil {
		t.Fatalf("CreateUnassigned: %v", err)
	}
	flow = newFlow(t, repo, &stubEligibility{ok: map[string]bool{}}, &stubOrders{byID: map[string]*order.Order{
		parentOrder.ID: parentOrder,
		childOrder.ID:  childOrder,
	}}, &stubVariants{byID: map[string]*catalog.Variant{
		"card-var":   {ID: "card-var", ProductID: "card-prod"},
		"ticket-var": {ID: "ticket-var", ProductID: "ticket-prod"},
	}}).WithLinkedLeaves(stubProducts{byID: map[string]*catalog.Product{
		"ticket-prod": {ID: "ticket-prod", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}, stubComponents{})
	_, err = adminBind(flow, childUnit, parentUnit)
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), app.ParentNotEligibleMessage) {
		t.Fatalf("unassigned ineligible parent = %v", err)
	}
}

func TestFlow_AdminAssign_RefusesUnpaidWithoutForce(t *testing.T) {
	parent := mustOrder(t, "cust-1", "card-var")
	item, err := order.NewItem("ticket-var", "SKU-1", "Ticket", 1, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	childOrder, err := order.NewOrder(id.New(), "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	child := &childOrder
	flow := newFlow(t, nil, &stubEligibility{ok: map[string]bool{"ticket-prod|card-prod": true}}, &stubOrders{byID: map[string]*order.Order{
		parent.ID: parent,
		child.ID:  child,
	}}, &stubVariants{byID: map[string]*catalog.Variant{
		"card-var":   {ID: "card-var", ProductID: "card-prod"},
		"ticket-var": {ID: "ticket-var", ProductID: "ticket-prod"},
	}})
	flow = linkChild(flow, "ticket-prod")
	childUnit := assignment.UnitID(child.ID, "ticket-var")
	parentUnit := assignment.UnitID(parent.ID, "card-var")
	_, err = adminBind(flow, childUnit, parentUnit)
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), app.ChildOrderNotBindableMessage) {
		t.Fatalf("unpaid = %v", err)
	}
	got, err := flow.AdminAssign(context.Background(), app.AdminAssignRequest{
		ChildUnitID:  childUnit,
		ParentUnitID: parentUnit,
		Force:        true,
		Reason:       "bind an unpaid ticket",
	})
	if err != nil {
		t.Fatalf("force unpaid: %v", err)
	}
	if !got.Created || got.Reason != "bind an unpaid ticket" {
		t.Fatalf("got %+v", got)
	}
}

func TestFlow_AdminAssign_ForceNotApplicableWhenBindable(t *testing.T) {
	parentOrder := mustOrder(t, "cust-1", "card-var")
	childOrder := mustOrder(t, "cust-1", "ticket-var")
	repo := &stubRepo{}
	flow := linkChild(newFlow(t, repo, &stubEligibility{ok: map[string]bool{"ticket-prod|card-prod": true}}, &stubOrders{byID: map[string]*order.Order{
		parentOrder.ID: parentOrder,
		childOrder.ID:  childOrder,
	}}, &stubVariants{byID: map[string]*catalog.Variant{
		"card-var":   {ID: "card-var", ProductID: "card-prod"},
		"ticket-var": {ID: "ticket-var", ProductID: "ticket-prod"},
	}}), "ticket-prod")
	_, err := flow.AdminAssign(context.Background(), app.AdminAssignRequest{
		ChildUnitID:  assignment.UnitID(childOrder.ID, "ticket-var"),
		ParentUnitID: assignment.UnitID(parentOrder.ID, "card-var"),
		Force:        true,
		Reason:       "not needed",
	})
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), app.ForceNotApplicableMessage) {
		t.Fatalf("force on bindable pair = %v", err)
	}
	if len(repo.inserts) != 0 {
		t.Fatalf("inserts = %d, want none", len(repo.inserts))
	}
}

func TestFlow_AdminAssign_ForceReasonTooLong(t *testing.T) {
	parentOrder := mustOrder(t, "cust-1", "card-var")
	childOrder := mustOrder(t, "cust-1", "ticket-var")
	orders := &stubOrders{byID: map[string]*order.Order{
		parentOrder.ID: parentOrder,
		childOrder.ID:  childOrder,
	}}
	flow := newFlow(t, nil, &stubEligibility{}, orders, &stubVariants{})
	_, err := flow.AdminAssign(context.Background(), app.AdminAssignRequest{
		ChildUnitID:  assignment.UnitID(childOrder.ID, "ticket-var"),
		ParentUnitID: assignment.UnitID(parentOrder.ID, "card-var"),
		Force:        true,
		Reason:       strings.Repeat("x", assignment.MaxForcedReasonLen+1),
	})
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), assignment.ForcedReasonTooLongMessage) {
		t.Fatalf("long reason = %v", err)
	}
	if orders.calls != 0 || orders.batchCalls != 0 {
		t.Fatalf("long reason looked up orders calls=%d batch=%d", orders.calls, orders.batchCalls)
	}
}

func TestFlow_FactsForUnits_BatchesOrders(t *testing.T) {
	first := mustOrder(t, "cust-1", "card-var")
	second := mustOrder(t, "cust-2", "ticket-var")
	orders := &stubOrders{byID: map[string]*order.Order{
		first.ID:  first,
		second.ID: second,
	}}
	flow := newFlow(t, nil, nil, orders, nil)
	unitA := assignment.UnitID(first.ID, "card-var")
	unitB := assignment.UnitID(first.ID, "other-var")
	unitC := assignment.UnitID(second.ID, "ticket-var")
	got, err := flow.FactsForUnits(context.Background(), []string{unitA, unitB, unitC, unitA, "not-a-unit"})
	if err != nil {
		t.Fatalf("FactsForUnits: %v", err)
	}
	if orders.batchCalls != 1 {
		t.Fatalf("FindByIDs calls = %d, want 1", orders.batchCalls)
	}
	if orders.calls != 0 {
		t.Fatalf("FindByID calls = %d, want 0", orders.calls)
	}
	if len(got) != 3 || got[unitA].CustomerID != "cust-1" || got[unitC].CustomerID != "cust-2" {
		t.Fatalf("facts = %+v", got)
	}
}

type orderFindByIDOnly struct {
	byID  map[string]*order.Order
	calls int
}

func (s *orderFindByIDOnly) FindByID(_ context.Context, id string) (*order.Order, error) {
	s.calls++
	return s.byID[id], nil
}

func (s *orderFindByIDOnly) FindByCustomerID(context.Context, string) ([]order.Order, error) {
	return nil, nil
}

type orderFactsByStoredID struct {
	byID map[string]*order.Order
}

func (s *orderFactsByStoredID) FindByID(_ context.Context, id string) (*order.Order, error) {
	if s == nil || s.byID == nil {
		return nil, nil
	}
	if o := s.byID[id]; o != nil {
		return o, nil
	}
	return s.byID[strings.ToLower(id)], nil
}

func (s *orderFactsByStoredID) FindByCustomerID(context.Context, string) ([]order.Order, error) {
	return nil, nil
}

func (s *orderFactsByStoredID) FindByIDs(_ context.Context, ids []string) (map[string]*order.Order, error) {
	out := make(map[string]*order.Order, len(ids))
	for _, id := range ids {
		o := s.byID[id]
		if o == nil {
			o = s.byID[strings.ToLower(id)]
		}
		if o != nil {
			out[o.ID] = o
		}
	}
	return out, nil
}

func TestFlow_FactsForUnits_CanonicalizesOrderID(t *testing.T) {
	item, err := order.NewItem("card-var", "SKU-1", "Card", 1, shared.MustNewMoney(0, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	firstOrder, err := order.NewOrder("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "cust-9", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	first := &firstOrder
	orders := &orderFactsByStoredID{byID: map[string]*order.Order{first.ID: first}}
	flow := app.NewFlow(app.NewService(&stubRepo{}), &stubRepo{}, &stubEligibility{}, orders, &stubVariants{}, &stubPayments{})
	upper := assignment.UnitID(strings.ToUpper(first.ID), "card-var")
	got, err := flow.FactsForUnits(context.Background(), []string{upper})
	if err != nil {
		t.Fatalf("FactsForUnits: %v", err)
	}
	canon, ok := assignment.CanonicalUnitID(upper)
	if !ok {
		t.Fatalf("CanonicalUnitID(%q) failed", upper)
	}
	if got[canon].CustomerID != "cust-9" || got[canon].OrderStatus == "" {
		t.Fatalf("facts = %+v, want customer cust-9 keyed by %q", got, canon)
	}
	if upper == canon {
		t.Fatal("fixture order id did not change case")
	}
	if _, found := got[upper]; found {
		t.Fatalf("facts keyed by original case %q: %+v", upper, got)
	}
}

func TestFlow_FactsForUnits_FallsBackToFindByID(t *testing.T) {
	first := mustOrder(t, "cust-1", "card-var")
	second := mustOrder(t, "cust-2", "ticket-var")
	orders := &orderFindByIDOnly{byID: map[string]*order.Order{
		first.ID:  first,
		second.ID: second,
	}}
	flow := app.NewFlow(app.NewService(&stubRepo{}), &stubRepo{}, &stubEligibility{}, orders, &stubVariants{}, &stubPayments{})
	_, err := flow.FactsForUnits(context.Background(), []string{
		assignment.UnitID(first.ID, "card-var"),
		assignment.UnitID(second.ID, "ticket-var"),
		assignment.UnitID(first.ID, "card-var"),
	})
	if err != nil {
		t.Fatalf("FactsForUnits: %v", err)
	}
	if orders.calls != 2 {
		t.Fatalf("FindByID calls = %d, want 2 unique orders", orders.calls)
	}
}

func TestFlow_AdminAssign_ForceRequiresReason(t *testing.T) {
	parentOrder := mustOrder(t, "cust-1", "card-var")
	childOrder := mustOrder(t, "cust-1", "ticket-var")
	orders := &stubOrders{byID: map[string]*order.Order{
		parentOrder.ID: parentOrder,
		childOrder.ID:  childOrder,
	}}
	flow := newFlow(t, nil, &stubEligibility{}, orders, &stubVariants{})
	_, err := flow.AdminAssign(context.Background(), app.AdminAssignRequest{
		ChildUnitID:  assignment.UnitID(childOrder.ID, "ticket-var"),
		ParentUnitID: assignment.UnitID(parentOrder.ID, "card-var"),
		Force:        true,
	})
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), app.ForceReasonRequiredMessage) {
		t.Fatalf("force without reason = %v", err)
	}
	if orders.calls != 0 {
		t.Fatalf("force without reason looked up orders %d times", orders.calls)
	}
}

func TestFlow_RecordPurchase_CanonicalizesParentUnit(t *testing.T) {
	parentOrder := mustOrder(t, "cust-1", "card-var")
	childOrder := mustOrder(t, "cust-1", "ticket-var")
	parentUnit := assignment.UnitID(parentOrder.ID, "card-var")
	repo := &stubRepo{}
	flow := newFlow(t, repo, &stubEligibility{ok: map[string]bool{"ticket|card-prod": true}}, &stubOrders{byID: map[string]*order.Order{
		parentOrder.ID: parentOrder,
		childOrder.ID:  childOrder,
	}}, &stubVariants{byID: map[string]*catalog.Variant{
		"card-var": {ID: "card-var", ProductID: "card-prod"},
	}})
	if err := flow.RecordPurchase(context.Background(), "cust-1", childOrder.ID, []app.PurchaseLine{{
		ChildVariantID: "ticket-var",
		ChildProductID: "ticket",
		Assignment:     catalog.AssignmentContext{ParentUnitID: " " + parentUnit + " "},
	}}); err != nil {
		t.Fatalf("RecordPurchase: %v", err)
	}
	if len(repo.inserts) != 1 || repo.inserts[0].ParentOrderItemID != parentUnit {
		t.Fatalf("inserts = %+v, want canonical parent", repo.inserts)
	}
}
