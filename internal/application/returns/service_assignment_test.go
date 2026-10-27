package returns_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"strings"
	"testing"

	assignmentapp "github.com/akarso/shopanda/internal/application/assignment"
	returnsApp "github.com/akarso/shopanda/internal/application/returns"
	"github.com/akarso/shopanda/internal/domain/assignment"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	domainReturns "github.com/akarso/shopanda/internal/domain/returns"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// assignmentStubRepo is a minimal ProductAssignment repository for returns
// integration tests (PR-1072). WithTx returns itself so Flow.BindsTx is true.
type assignmentStubRepo struct {
	byID     map[string]*assignment.ProductAssignment
	current  map[string]*assignment.ProductAssignment
	byParent map[string][]assignment.ProductAssignment
}

func (r *assignmentStubRepo) WithTx(*sql.Tx) assignment.Repository { return r }

func (r *assignmentStubRepo) clone() *assignmentStubRepo {
	out := &assignmentStubRepo{
		byID:     map[string]*assignment.ProductAssignment{},
		current:  map[string]*assignment.ProductAssignment{},
		byParent: map[string][]assignment.ProductAssignment{},
	}
	for k, v := range r.byID {
		cp := *v
		out.byID[k] = &cp
	}
	for k, v := range r.current {
		cp := *v
		out.current[k] = &cp
	}
	for k, rows := range r.byParent {
		cpRows := make([]assignment.ProductAssignment, len(rows))
		copy(cpRows, rows)
		out.byParent[k] = cpRows
	}
	return out
}

func (r *assignmentStubRepo) restore(snap *assignmentStubRepo) {
	r.byID = snap.byID
	r.current = snap.current
	r.byParent = snap.byParent
}

func (r *assignmentStubRepo) Insert(_ context.Context, a *assignment.ProductAssignment) error {
	if r.byID == nil {
		r.byID = map[string]*assignment.ProductAssignment{}
	}
	if r.current == nil {
		r.current = map[string]*assignment.ProductAssignment{}
	}
	cp := *a
	r.byID[a.ID] = &cp
	if a.Status().IsCurrent() {
		r.current[a.ChildOrderItemID] = &cp
	}
	if a.ParentOrderItemID != "" {
		if r.byParent == nil {
			r.byParent = map[string][]assignment.ProductAssignment{}
		}
		r.byParent[a.ParentOrderItemID] = append(r.byParent[a.ParentOrderItemID], cp)
	}
	return nil
}

func (r *assignmentStubRepo) Update(_ context.Context, a *assignment.ProductAssignment) error {
	a.AcceptStoredStatus()
	if r.byID == nil {
		r.byID = map[string]*assignment.ProductAssignment{}
	}
	if r.current == nil {
		r.current = map[string]*assignment.ProductAssignment{}
	}
	cp := *a
	r.byID[a.ID] = &cp
	if a.Status().IsCurrent() {
		r.current[a.ChildOrderItemID] = &cp
	} else {
		delete(r.current, a.ChildOrderItemID)
	}
	if a.ParentOrderItemID != "" {
		if r.byParent == nil {
			r.byParent = map[string][]assignment.ProductAssignment{}
		}
		parent := a.ParentOrderItemID
		rows := r.byParent[parent]
		for i := range rows {
			if rows[i].ID == a.ID {
				rows[i] = cp
				r.byParent[parent] = rows
				return nil
			}
		}
		r.byParent[parent] = append(r.byParent[parent], cp)
	}
	return nil
}

func (r *assignmentStubRepo) FindByID(_ context.Context, id string) (*assignment.ProductAssignment, error) {
	if r.byID == nil {
		return nil, nil
	}
	return r.byID[id], nil
}

func (r *assignmentStubRepo) FindCurrentByChildID(_ context.Context, childOrderItemID string) (*assignment.ProductAssignment, error) {
	if r.current == nil {
		return nil, nil
	}
	return r.current[childOrderItemID], nil
}

func (r *assignmentStubRepo) ListByChildID(context.Context, string) ([]assignment.ProductAssignment, error) {
	return nil, nil
}

func (r *assignmentStubRepo) ListByParentID(_ context.Context, parentOrderItemID string, page assignment.ParentPage) ([]assignment.ProductAssignment, bool, error) {
	rows := r.byParent[parentOrderItemID]
	status := page.Status
	if status == "" {
		status = string(assignment.StatusAssigned)
	}
	filtered := make([]assignment.ProductAssignment, 0, len(rows))
	for _, row := range rows {
		if status != "all" && string(row.Status()) != status {
			continue
		}
		filtered = append(filtered, row)
	}
	limit := page.Limit
	if limit <= 0 {
		limit = 100
	}
	if page.Offset >= len(filtered) {
		return []assignment.ProductAssignment{}, false, nil
	}
	rest := filtered[page.Offset:]
	if len(rest) > limit {
		return rest[:limit], true, nil
	}
	return rest, false, nil
}

type assignmentEligOK struct{}

func (assignmentEligOK) IsEligible(context.Context, string, string) (bool, error) { return true, nil }

type assignmentOrdersStub struct {
	byID map[string]*order.Order
}

func (s assignmentOrdersStub) FindByID(_ context.Context, id string) (*order.Order, error) {
	return s.byID[id], nil
}
func (assignmentOrdersStub) FindByCustomerID(context.Context, string) ([]order.Order, error) {
	return nil, nil
}

type assignmentVariantsStub struct {
	byID map[string]*catalog.Variant
}

func (s assignmentVariantsStub) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	if s.byID != nil {
		if v := s.byID[id]; v != nil {
			return v, nil
		}
	}
	return &catalog.Variant{ID: id, ProductID: "prod-" + id, SKU: "SKU-" + id}, nil
}

type assignmentPaymentsStub struct {
	byOrder map[string]*payment.Payment
}

func (s assignmentPaymentsStub) FindByOrderID(_ context.Context, orderID string) (*payment.Payment, error) {
	if s.byOrder != nil {
		return s.byOrder[orderID], nil
	}
	return nil, nil
}

type assignmentProductsStub struct{}

func (assignmentProductsStub) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	return &catalog.Product{ID: id, RequiresLinkedParent: true}, nil
}

func paidOrderWithVariants(t *testing.T, orderID string, variants ...string) *order.Order {
	t.Helper()
	items := make([]order.Item, 0, len(variants))
	for i, v := range variants {
		item, err := order.NewItem(v, "SKU-"+v, "Item "+v, 1, shared.MustNewMoney(1000, "EUR"))
		if err != nil {
			t.Fatalf("NewItem %d: %v", i, err)
		}
		item.Restockable = true
		items = append(items, item)
	}
	ord, err := order.NewOrder(orderID, "c1", "", "EUR", items)
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	if err := ord.Confirm(); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if err := ord.MarkPaid(); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}
	return &ord
}

func completedPayment(t *testing.T, orderID string) *payment.Payment {
	t.Helper()
	pay, err := payment.NewPayment("pay-"+orderID, orderID, payment.MethodStripe, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("NewPayment: %v", err)
	}
	_ = pay.Complete("pi_" + orderID)
	return &pay
}

type returnsWithAssignments struct {
	svc       *returnsApp.Service
	assignSvc *assignmentapp.Service
	flow      *assignmentapp.Flow
	repo      *assignmentStubRepo
	bus       *event.Bus
}

func newReturnsWithAssignments(t *testing.T, ord *order.Order, assignRepo *assignmentStubRepo) returnsWithAssignments {
	t.Helper()
	if assignRepo == nil {
		assignRepo = &assignmentStubRepo{}
	}
	pay := completedPayment(t, ord.ID)
	assignSvc := assignmentapp.NewService(assignRepo)
	flow := assignmentapp.NewFlow(
		assignSvc,
		assignRepo,
		assignmentEligOK{},
		assignmentOrdersStub{byID: map[string]*order.Order{ord.ID: ord}},
		assignmentVariantsStub{},
		assignmentPaymentsStub{byOrder: map[string]*payment.Payment{ord.ID: pay}},
	).WithLinkedLeaves(assignmentProductsStub{}, nil)
	returns := newMemReturnRepo()
	stock := &memStockRepo{qty: map[string]int{}}
	for _, item := range ord.Items() {
		stock.qty[item.VariantID] = 5
	}
	bus := event.NewBus(logger.NewWithWriter(io.Discard, "info"))
	svc := returnsApp.NewService(
		returns,
		&memOrderRepo{order: ord},
		stock,
		memVariantRepo{},
		&memPaymentRepo{payment: pay},
		&stubRefunder{},
		bus,
		logger.NewWithWriter(io.Discard, "info"),
	).WithTxStarter(returnsFakeTxDB()).WithAssignmentFlow(flow)
	flow.WithOpenReturnChecker(svc)
	return returnsWithAssignments{svc: svc, assignSvc: assignSvc, flow: flow, repo: assignRepo, bus: bus}
}

func currentOrNil(t *testing.T, svc *assignmentapp.Service, childUnit string) *assignment.ProductAssignment {
	t.Helper()
	current, err := svc.CurrentByChild(context.Background(), childUnit)
	if err != nil {
		t.Fatalf("CurrentByChild: %v", err)
	}
	return current
}

func TestService_Approve_RevokesAssignedChild(t *testing.T) {
	cardOrderID := id.New()
	ticketOrderID := id.New()
	cardOrd := paidOrderWithVariants(t, cardOrderID, "card-var")
	ticketOrd := paidOrderWithVariants(t, ticketOrderID, "ticket-var")
	parentUnit := assignment.UnitID(cardOrderID, "card-var")
	childUnit := assignment.UnitID(ticketOrderID, "ticket-var")

	h := newReturnsWithAssignments(t, ticketOrd, &assignmentStubRepo{})
	if _, err := h.assignSvc.CreateAssigned(context.Background(), parentUnit, childUnit); err != nil {
		t.Fatalf("CreateAssigned: %v", err)
	}

	var revokedReasons []string
	h.bus.On(assignment.EventAssignmentRevoked, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(assignment.AssignmentRevokedData)
		if !ok {
			t.Fatalf("event data type = %T", evt.Data)
		}
		revokedReasons = append(revokedReasons, data.Reason)
		return nil
	})

	ret, err := h.svc.RequestReturn(context.Background(), ticketOrderID, "c1", "unused", []returnsApp.RequestLine{
		{VariantID: "ticket-var", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}
	if len(ret.Items()) != 1 || ret.Items()[0].VariantID != "ticket-var" {
		t.Fatalf("items = %+v", ret.Items())
	}

	ret, err = h.svc.Approve(context.Background(), ret.ID)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if ret.Status() != domainReturns.StatusApproved {
		t.Fatalf("status = %q", ret.Status())
	}
	if currentOrNil(t, h.assignSvc, childUnit) != nil {
		t.Fatal("assignment still current after approve")
	}
	if len(revokedReasons) != 1 || revokedReasons[0] != assignmentapp.RevokeReasonReturnApproved {
		t.Fatalf("revoke events = %v, want [%s]", revokedReasons, assignmentapp.RevokeReasonReturnApproved)
	}
	_ = cardOrd
}

func TestService_Approve_RevokesUnassignedChild(t *testing.T) {
	ticketOrderID := id.New()
	ticketOrd := paidOrderWithVariants(t, ticketOrderID, "ticket-var")
	childUnit := assignment.UnitID(ticketOrderID, "ticket-var")

	h := newReturnsWithAssignments(t, ticketOrd, &assignmentStubRepo{})
	if _, err := h.assignSvc.CreateUnassigned(context.Background(), childUnit); err != nil {
		t.Fatalf("CreateUnassigned: %v", err)
	}

	ret, err := h.svc.RequestReturn(context.Background(), ticketOrderID, "c1", "unused", []returnsApp.RequestLine{
		{VariantID: "ticket-var", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}
	if _, err := h.svc.Approve(context.Background(), ret.ID); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if currentOrNil(t, h.assignSvc, childUnit) != nil {
		t.Fatal("expected nil current after approve")
	}
}

func TestService_RequestReturn_DoesNotRevoke(t *testing.T) {
	ticketOrderID := id.New()
	ticketOrd := paidOrderWithVariants(t, ticketOrderID, "ticket-var")
	childUnit := assignment.UnitID(ticketOrderID, "ticket-var")
	parentUnit := assignment.UnitID(id.New(), "card-var")

	h := newReturnsWithAssignments(t, ticketOrd, &assignmentStubRepo{})
	if _, err := h.assignSvc.CreateAssigned(context.Background(), parentUnit, childUnit); err != nil {
		t.Fatalf("CreateAssigned: %v", err)
	}

	if _, err := h.svc.RequestReturn(context.Background(), ticketOrderID, "c1", "unused", []returnsApp.RequestLine{
		{VariantID: "ticket-var", Quantity: 1},
	}); err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}
	cur := currentOrNil(t, h.assignSvc, childUnit)
	if cur == nil || cur.Status() != assignment.StatusAssigned {
		t.Fatalf("current = %+v, want still assigned", cur)
	}
}

func TestService_RequestReturn_BlocksParentWithAssignedChildren(t *testing.T) {
	cardOrderID := id.New()
	ticketOrderID := id.New()
	cardOrd := paidOrderWithVariants(t, cardOrderID, "card-var")
	parentUnit := assignment.UnitID(cardOrderID, "card-var")
	childUnit := assignment.UnitID(ticketOrderID, "ticket-var")

	h := newReturnsWithAssignments(t, cardOrd, &assignmentStubRepo{})
	if _, err := h.assignSvc.CreateAssigned(context.Background(), parentUnit, childUnit); err != nil {
		t.Fatalf("CreateAssigned: %v", err)
	}

	_, err := h.svc.RequestReturn(context.Background(), cardOrderID, "c1", "damaged", []returnsApp.RequestLine{
		{VariantID: "card-var", Quantity: 1},
	})
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), assignmentapp.ParentHasAssignedChildrenMessage) {
		t.Fatalf("err = %v, want parent-has-children", err)
	}
	if !strings.Contains(err.Error(), childUnit) {
		t.Fatalf("err = %v, want required child unit %q", err, childUnit)
	}
}

func TestService_RequestReturn_SameRMACardAndTickets(t *testing.T) {
	orderID := id.New()
	ord := paidOrderWithVariants(t, orderID, "card-var", "ticket-var")
	parentUnit := assignment.UnitID(orderID, "card-var")
	childUnit := assignment.UnitID(orderID, "ticket-var")

	h := newReturnsWithAssignments(t, ord, &assignmentStubRepo{})
	if _, err := h.assignSvc.CreateAssigned(context.Background(), parentUnit, childUnit); err != nil {
		t.Fatalf("CreateAssigned: %v", err)
	}

	ret, err := h.svc.RequestReturn(context.Background(), orderID, "c1", "unused", []returnsApp.RequestLine{
		{VariantID: "card-var", Quantity: 1},
		{VariantID: "ticket-var", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("RequestReturn same-RMA: %v", err)
	}
	if len(ret.Items()) != 2 {
		t.Fatalf("items = %d, want 2", len(ret.Items()))
	}
	if _, err := h.svc.Approve(context.Background(), ret.ID); err != nil {
		t.Fatalf("Approve same-RMA: %v", err)
	}
	if currentOrNil(t, h.assignSvc, childUnit) != nil {
		t.Fatal("ticket assignment should be revoked after same-RMA approve")
	}
}

func TestService_RequestReturn_AllowsParentAfterChildrenRevoked(t *testing.T) {
	cardOrderID := id.New()
	ticketOrderID := id.New()
	cardOrd := paidOrderWithVariants(t, cardOrderID, "card-var")
	parentUnit := assignment.UnitID(cardOrderID, "card-var")
	childUnit := assignment.UnitID(ticketOrderID, "ticket-var")

	h := newReturnsWithAssignments(t, cardOrd, &assignmentStubRepo{})
	a, err := h.assignSvc.CreateAssigned(context.Background(), parentUnit, childUnit)
	if err != nil {
		t.Fatalf("CreateAssigned: %v", err)
	}
	if _, _, err := h.assignSvc.Revoke(context.Background(), a.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	ret, err := h.svc.RequestReturn(context.Background(), cardOrderID, "c1", "damaged", []returnsApp.RequestLine{
		{VariantID: "card-var", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("RequestReturn after revoke: %v", err)
	}
	if ret.Status() != domainReturns.StatusRequested {
		t.Fatalf("status = %q", ret.Status())
	}
}

func TestService_Approve_RejectedKeepsAssignment(t *testing.T) {
	ticketOrderID := id.New()
	ticketOrd := paidOrderWithVariants(t, ticketOrderID, "ticket-var")
	childUnit := assignment.UnitID(ticketOrderID, "ticket-var")
	parentUnit := assignment.UnitID(id.New(), "card-var")

	h := newReturnsWithAssignments(t, ticketOrd, &assignmentStubRepo{})
	if _, err := h.assignSvc.CreateAssigned(context.Background(), parentUnit, childUnit); err != nil {
		t.Fatalf("CreateAssigned: %v", err)
	}

	ret, err := h.svc.RequestReturn(context.Background(), ticketOrderID, "c1", "unused", []returnsApp.RequestLine{
		{VariantID: "ticket-var", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}
	if _, err := h.svc.Reject(context.Background(), ret.ID); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	_, err = h.svc.Approve(context.Background(), ret.ID)
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("Approve on rejected = %v, want validation", err)
	}
	cur := currentOrNil(t, h.assignSvc, childUnit)
	if cur == nil || cur.Status() != assignment.StatusAssigned {
		t.Fatalf("current = %+v, want still assigned", cur)
	}
}

func TestService_Refund_RequestedKeepsAssignment(t *testing.T) {
	ticketOrderID := id.New()
	ticketOrd := paidOrderWithVariants(t, ticketOrderID, "ticket-var")
	childUnit := assignment.UnitID(ticketOrderID, "ticket-var")
	parentUnit := assignment.UnitID(id.New(), "card-var")

	h := newReturnsWithAssignments(t, ticketOrd, &assignmentStubRepo{})
	if _, err := h.assignSvc.CreateAssigned(context.Background(), parentUnit, childUnit); err != nil {
		t.Fatalf("CreateAssigned: %v", err)
	}

	ret, err := h.svc.RequestReturn(context.Background(), ticketOrderID, "c1", "unused", []returnsApp.RequestLine{
		{VariantID: "ticket-var", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}
	_, err = h.svc.Refund(context.Background(), ret.ID)
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("Refund on requested = %v, want validation", err)
	}
	cur := currentOrNil(t, h.assignSvc, childUnit)
	if cur == nil || cur.Status() != assignment.StatusAssigned {
		t.Fatalf("current = %+v, want still assigned", cur)
	}
}

func TestService_Receive_ReChecksParentGate(t *testing.T) {
	orderID := id.New()
	ord := paidOrderWithVariants(t, orderID, "card-var", "ticket-var")
	parentUnit := assignment.UnitID(orderID, "card-var")
	childUnit := assignment.UnitID(orderID, "ticket-var")

	h := newReturnsWithAssignments(t, ord, &assignmentStubRepo{})
	ret, err := h.svc.RequestReturn(context.Background(), orderID, "c1", "damaged", []returnsApp.RequestLine{
		{VariantID: "card-var", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}
	if _, err := h.svc.Approve(context.Background(), ret.ID); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	// Assignment lands after approve — Receive must refuse.
	if _, err := h.assignSvc.CreateAssigned(context.Background(), parentUnit, childUnit); err != nil {
		t.Fatalf("CreateAssigned after approve: %v", err)
	}
	_, err = h.svc.Receive(context.Background(), ret.ID)
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), assignmentapp.ParentHasAssignedChildrenMessage) {
		t.Fatalf("Receive = %v, want parent gate", err)
	}
}

func TestService_Refund_ReChecksParentGate(t *testing.T) {
	orderID := id.New()
	ord := paidOrderWithVariants(t, orderID, "card-var", "ticket-var")
	parentUnit := assignment.UnitID(orderID, "card-var")
	childUnit := assignment.UnitID(orderID, "ticket-var")

	h := newReturnsWithAssignments(t, ord, &assignmentStubRepo{})
	ret, err := h.svc.RequestReturn(context.Background(), orderID, "c1", "damaged", []returnsApp.RequestLine{
		{VariantID: "card-var", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}
	if _, err := h.svc.Approve(context.Background(), ret.ID); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if _, err := h.svc.Receive(context.Background(), ret.ID); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if _, err := h.assignSvc.CreateAssigned(context.Background(), parentUnit, childUnit); err != nil {
		t.Fatalf("CreateAssigned after receive: %v", err)
	}
	_, err = h.svc.Refund(context.Background(), ret.ID)
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), assignmentapp.ParentHasAssignedChildrenMessage) {
		t.Fatalf("Refund = %v, want parent gate", err)
	}
}

func TestService_ReturnableLines_HidesBlockedCard(t *testing.T) {
	orderID := id.New()
	ord := paidOrderWithVariants(t, orderID, "card-var", "other-var")
	parentUnit := assignment.UnitID(orderID, "card-var")
	// Child on a different order — cannot same-RMA with the card.
	otherOrderID := id.New()
	childUnit := assignment.UnitID(otherOrderID, "ticket-var")

	h := newReturnsWithAssignments(t, ord, &assignmentStubRepo{})
	if _, err := h.assignSvc.CreateAssigned(context.Background(), parentUnit, childUnit); err != nil {
		t.Fatalf("CreateAssigned: %v", err)
	}

	lines, err := h.svc.ReturnableLines(context.Background(), orderID, "c1")
	if err != nil {
		t.Fatalf("ReturnableLines: %v", err)
	}
	for _, line := range lines {
		if line.VariantID == "card-var" {
			t.Fatalf("card-var still returnable while blocked: %+v", lines)
		}
	}
	foundOther := false
	for _, line := range lines {
		if line.VariantID == "other-var" {
			foundOther = true
		}
	}
	if !foundOther {
		t.Fatalf("expected other-var still returnable, got %+v", lines)
	}
}

func TestService_ReturnableLines_ShowsCardWhenTicketsAlsoReturnable(t *testing.T) {
	orderID := id.New()
	ord := paidOrderWithVariants(t, orderID, "card-var", "ticket-var")
	parentUnit := assignment.UnitID(orderID, "card-var")
	childUnit := assignment.UnitID(orderID, "ticket-var")

	h := newReturnsWithAssignments(t, ord, &assignmentStubRepo{})
	if _, err := h.assignSvc.CreateAssigned(context.Background(), parentUnit, childUnit); err != nil {
		t.Fatalf("CreateAssigned: %v", err)
	}

	lines, err := h.svc.ReturnableLines(context.Background(), orderID, "c1")
	if err != nil {
		t.Fatalf("ReturnableLines: %v", err)
	}
	foundCard, foundTicket := false, false
	for _, line := range lines {
		if line.VariantID == "card-var" {
			foundCard = true
		}
		if line.VariantID == "ticket-var" {
			foundTicket = true
		}
	}
	if !foundCard || !foundTicket {
		t.Fatalf("want card+ticket returnable for same-RMA, got %+v", lines)
	}
}

func TestService_Approve_PersistFailureRollsBackAssignment(t *testing.T) {
	ticketOrderID := id.New()
	ticketOrd := paidOrderWithVariants(t, ticketOrderID, "ticket-var")
	childUnit := assignment.UnitID(ticketOrderID, "ticket-var")
	parentUnit := assignment.UnitID(id.New(), "card-var")

	assignRepo := &assignmentStubRepo{}
	assignSvc := assignmentapp.NewService(assignRepo)
	pay := completedPayment(t, ticketOrderID)
	flow := assignmentapp.NewFlow(
		assignSvc,
		assignRepo,
		assignmentEligOK{},
		assignmentOrdersStub{byID: map[string]*order.Order{ticketOrderID: ticketOrd}},
		assignmentVariantsStub{},
		assignmentPaymentsStub{byOrder: map[string]*payment.Payment{ticketOrderID: pay}},
	)
	if _, err := assignSvc.CreateAssigned(context.Background(), parentUnit, childUnit); err != nil {
		t.Fatalf("CreateAssigned: %v", err)
	}

	baseReturns := newMemReturnRepo()
	failing := &failUpdateReturnRepo{memReturnRepo: baseReturns, err: errors.New("persist boom")}
	stock := &memStockRepo{qty: map[string]int{"ticket-var": 5}}
	bus := event.NewBus(logger.NewWithWriter(io.Discard, "info"))

	returnsCaptureCommits.Store(0)
	db, err := sql.Open("shopanda-returns-capture-tx", "")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	snap := assignRepo.clone()
	returnsCaptureFinished = func() {
		if returnsCaptureCommits.Load() == 0 {
			assignRepo.restore(snap)
		}
	}
	t.Cleanup(func() { returnsCaptureFinished = nil })

	svc := returnsApp.NewService(
		failing,
		&memOrderRepo{order: ticketOrd},
		stock,
		memVariantRepo{},
		&memPaymentRepo{payment: pay},
		&stubRefunder{},
		bus,
		logger.NewWithWriter(io.Discard, "info"),
	).WithTxStarter(db).WithAssignmentFlow(flow)

	ret, err := svc.RequestReturn(context.Background(), ticketOrderID, "c1", "unused", []returnsApp.RequestLine{
		{VariantID: "ticket-var", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}
	// Snapshot after request (assignment still assigned) for rollback restore.
	snap = assignRepo.clone()

	_, err = svc.Approve(context.Background(), ret.ID)
	if err == nil {
		t.Fatal("expected Approve persist failure")
	}
	cur := currentOrNil(t, assignSvc, childUnit)
	if cur == nil || cur.Status() != assignment.StatusAssigned {
		t.Fatalf("current after failed approve = %+v, want still assigned", cur)
	}
	if returnsCaptureCommits.Load() != 0 {
		t.Fatalf("commits = %d, want 0", returnsCaptureCommits.Load())
	}
}

type failUpdateReturnRepo struct {
	*memReturnRepo
	err error
}

func (r *failUpdateReturnRepo) Update(ctx context.Context, ret *domainReturns.Return) error {
	if r.err != nil {
		return r.err
	}
	return r.memReturnRepo.Update(ctx, ret)
}

func (r *failUpdateReturnRepo) WithTx(*sql.Tx) domainReturns.Repository { return r }

func TestService_Refund_RevokesIdempotently(t *testing.T) {
	ticketOrderID := id.New()
	ticketOrd := paidOrderWithVariants(t, ticketOrderID, "ticket-var")
	childUnit := assignment.UnitID(ticketOrderID, "ticket-var")
	parentUnit := assignment.UnitID(id.New(), "card-var")

	h := newReturnsWithAssignments(t, ticketOrd, &assignmentStubRepo{})
	if _, err := h.assignSvc.CreateAssigned(context.Background(), parentUnit, childUnit); err != nil {
		t.Fatalf("CreateAssigned: %v", err)
	}

	ret, err := h.svc.RequestReturn(context.Background(), ticketOrderID, "c1", "unused", []returnsApp.RequestLine{
		{VariantID: "ticket-var", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}
	if _, err := h.svc.Approve(context.Background(), ret.ID); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if _, err := h.svc.Receive(context.Background(), ret.ID); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	// Re-bind via state service (bypasses open-RMA gate) to exercise refund safety net.
	if _, err := h.assignSvc.CreateAssigned(context.Background(), parentUnit, childUnit); err != nil {
		t.Fatalf("re-CreateAssigned: %v", err)
	}
	var refundReasons []string
	h.bus.On(assignment.EventAssignmentRevoked, func(_ context.Context, evt event.Event) error {
		data := evt.Data.(assignment.AssignmentRevokedData)
		refundReasons = append(refundReasons, data.Reason)
		return nil
	})
	if _, err := h.svc.Refund(context.Background(), ret.ID); err != nil {
		t.Fatalf("Refund: %v", err)
	}
	if currentOrNil(t, h.assignSvc, childUnit) != nil {
		t.Fatal("current after refund should be nil")
	}
	if len(refundReasons) != 1 || refundReasons[0] != assignmentapp.RevokeReasonReturnRefunded {
		t.Fatalf("refund revoke events = %v", refundReasons)
	}
}

func TestFlow_AssignOwned_BlockedWhileOnOpenReturn(t *testing.T) {
	orderID := id.New()
	ord := paidOrderWithVariants(t, orderID, "card-var", "ticket-var")
	parentUnit := assignment.UnitID(orderID, "card-var")
	childUnit := assignment.UnitID(orderID, "ticket-var")

	h := newReturnsWithAssignments(t, ord, &assignmentStubRepo{})
	if _, err := h.assignSvc.CreateUnassigned(context.Background(), childUnit); err != nil {
		t.Fatalf("CreateUnassigned: %v", err)
	}

	ret, err := h.svc.RequestReturn(context.Background(), orderID, "c1", "unused", []returnsApp.RequestLine{
		{VariantID: "card-var", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}
	if _, err := h.svc.Approve(context.Background(), ret.ID); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	_, err = h.flow.AssignOwned(context.Background(), "c1", childUnit, parentUnit)
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), assignmentapp.UnitOnOpenReturnMessage) {
		t.Fatalf("AssignOwned = %v, want open-return block", err)
	}
}

func TestFlow_AdminAssign_BlockedWhileOnOpenReturn(t *testing.T) {
	orderID := id.New()
	ord := paidOrderWithVariants(t, orderID, "card-var", "ticket-var")
	parentUnit := assignment.UnitID(orderID, "card-var")
	childUnit := assignment.UnitID(orderID, "ticket-var")

	h := newReturnsWithAssignments(t, ord, &assignmentStubRepo{})
	if _, err := h.assignSvc.CreateUnassigned(context.Background(), childUnit); err != nil {
		t.Fatalf("CreateUnassigned: %v", err)
	}

	if _, err := h.svc.RequestReturn(context.Background(), orderID, "c1", "unused", []returnsApp.RequestLine{
		{VariantID: "card-var", Quantity: 1},
	}); err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}

	_, err := h.flow.AdminAssign(context.Background(), assignmentapp.AdminAssignRequest{
		ChildUnitID:  childUnit,
		ParentUnitID: parentUnit,
	})
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), assignmentapp.UnitOnOpenReturnMessage) {
		t.Fatalf("AdminAssign = %v, want open-return block", err)
	}
}

func TestFlow_RecordPurchase_BlockedWhileParentOnOpenReturn(t *testing.T) {
	cardOrderID := id.New()
	cardOrd := paidOrderWithVariants(t, cardOrderID, "card-var")
	parentUnit := assignment.UnitID(cardOrderID, "card-var")

	h := newReturnsWithAssignments(t, cardOrd, &assignmentStubRepo{})
	ret, err := h.svc.RequestReturn(context.Background(), cardOrderID, "c1", "unused", []returnsApp.RequestLine{
		{VariantID: "card-var", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}
	if ret.Status() != domainReturns.StatusRequested {
		t.Fatalf("status = %s, want requested", ret.Status())
	}

	newOrderID := id.New()
	err = h.flow.CheckPurchase(context.Background(), "c1", "ticket-prod", catalog.AssignmentContext{
		ParentUnitID:    parentUnit,
		ParentProductID: "prod-card-var",
	})
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), assignmentapp.UnitOnOpenReturnMessage) {
		t.Fatalf("CheckPurchase = %v, want open-return block", err)
	}

	err = h.flow.RecordPurchase(context.Background(), "c1", newOrderID, []assignmentapp.PurchaseLine{{
		ChildVariantID: "ticket-var",
		ChildProductID: "ticket-prod",
		Assignment: catalog.AssignmentContext{
			ParentUnitID:    parentUnit,
			ParentProductID: "prod-card-var",
		},
	}})
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), assignmentapp.UnitOnOpenReturnMessage) {
		t.Fatalf("RecordPurchase = %v, want open-return block", err)
	}
	if cur := currentOrNil(t, h.assignSvc, assignment.UnitID(newOrderID, "ticket-var")); cur != nil {
		t.Fatalf("unexpected assignment row after blocked purchase: %+v", cur)
	}
	if len(h.repo.byID) != 0 {
		t.Fatalf("assignment repo should stay empty, got %d rows", len(h.repo.byID))
	}
}

func TestFlow_ListOwnedParents_HidesParentOnOpenReturn(t *testing.T) {
	orderID := id.New()
	ord := paidOrderWithVariants(t, orderID, "card-var")
	parentUnit := assignment.UnitID(orderID, "card-var")
	pay := completedPayment(t, orderID)

	assignRepo := &assignmentStubRepo{}
	assignSvc := assignmentapp.NewService(assignRepo)
	orders := &assignmentOrdersByCustomer{
		byID:       map[string]*order.Order{orderID: ord},
		byCustomer: map[string][]order.Order{"c1": {*ord}},
	}
	flow := assignmentapp.NewFlow(
		assignSvc,
		assignRepo,
		assignmentEligOK{},
		orders,
		assignmentVariantsStub{},
		assignmentPaymentsStub{byOrder: map[string]*payment.Payment{orderID: pay}},
	).WithLinkedLeaves(assignmentProductsStub{}, nil)

	returns := newMemReturnRepo()
	bus := event.NewBus(logger.NewWithWriter(io.Discard, "info"))
	svc := returnsApp.NewService(
		returns,
		&memOrderRepo{order: ord},
		&memStockRepo{qty: map[string]int{"card-var": 5}},
		memVariantRepo{},
		&memPaymentRepo{payment: pay},
		&stubRefunder{},
		bus,
		logger.NewWithWriter(io.Discard, "info"),
	).WithTxStarter(returnsFakeTxDB()).WithAssignmentFlow(flow)
	flow.WithOpenReturnChecker(svc)

	before, err := flow.ListOwnedParents(context.Background(), "c1", "ticket-prod")
	if err != nil {
		t.Fatalf("ListOwnedParents before RMA: %v", err)
	}
	if len(before) != 1 || before[0].UnitID != parentUnit {
		t.Fatalf("before = %+v, want parent listed", before)
	}

	if _, err := svc.RequestReturn(context.Background(), orderID, "c1", "unused", []returnsApp.RequestLine{
		{VariantID: "card-var", Quantity: 1},
	}); err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}

	after, err := flow.ListOwnedParents(context.Background(), "c1", "ticket-prod")
	if err != nil {
		t.Fatalf("ListOwnedParents after RMA: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("after open RMA = %+v, want empty", after)
	}
}

// assignmentOrdersByCustomer extends the single-order stub with customer list.
type assignmentOrdersByCustomer struct {
	byID       map[string]*order.Order
	byCustomer map[string][]order.Order
}

func (s *assignmentOrdersByCustomer) FindByID(_ context.Context, id string) (*order.Order, error) {
	return s.byID[id], nil
}

func (s *assignmentOrdersByCustomer) FindByCustomerID(_ context.Context, customerID string) ([]order.Order, error) {
	return s.byCustomer[customerID], nil
}
