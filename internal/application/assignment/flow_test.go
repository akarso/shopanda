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
}

func (s *stubOrders) FindByID(_ context.Context, id string) (*order.Order, error) {
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

type stubVariants struct {
	byID       map[string]*catalog.Variant
	idCalls    int
	batchCalls int
}

func (s *stubVariants) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	s.idCalls++
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
