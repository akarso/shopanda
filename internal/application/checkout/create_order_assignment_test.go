package checkout_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync"
	"testing"

	assignmentapp "github.com/akarso/shopanda/internal/application/assignment"
	"github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/domain/assignment"
	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
)

func init() {
	sql.Register("shopanda-checkout-assignment-fake-tx", assignmentFakeTxDriver{})
}

var (
	assignmentFakeTxOnce sync.Once
	assignmentFakeTxDB   *sql.DB
)

func assignmentTestDB() *sql.DB {
	assignmentFakeTxOnce.Do(func() {
		db, err := sql.Open("shopanda-checkout-assignment-fake-tx", "")
		if err != nil {
			panic(err)
		}
		assignmentFakeTxDB = db
	})
	return assignmentFakeTxDB
}

type assignmentFakeTxDriver struct{}

func (assignmentFakeTxDriver) Open(string) (driver.Conn, error) { return assignmentFakeTxConn{}, nil }

type assignmentFakeTxConn struct{}

func (assignmentFakeTxConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (assignmentFakeTxConn) Close() error                        { return nil }
func (assignmentFakeTxConn) Begin() (driver.Tx, error)           { return assignmentFakeTx{}, nil }

type assignmentFakeTx struct{}

func (assignmentFakeTx) Commit() error   { return nil }
func (assignmentFakeTx) Rollback() error { return nil }

type bindableOrderRepo struct {
	mockOrderRepo
}

func (r *bindableOrderRepo) WithTx(*sql.Tx) order.OrderRepository { return r }

func (r *bindableOrderRepo) FindByID(_ context.Context, id string) (*order.Order, error) {
	if r.saved != nil && r.saved.ID == id {
		return r.saved, nil
	}
	return nil, nil
}

func (r *bindableOrderRepo) FindByCustomerID(_ context.Context, customerID string) ([]order.Order, error) {
	if r.saved != nil && r.saved.CustomerID == customerID {
		return []order.Order{*r.saved}, nil
	}
	return nil, nil
}

type splitOrderRepo struct {
	mockOrderRepo
	txSaved *order.Order
}

func (r *splitOrderRepo) WithTx(*sql.Tx) order.OrderRepository {
	return &txOnlyOrderRepo{parent: r}
}

type txOnlyOrderRepo struct {
	mockOrderRepo
	parent *splitOrderRepo
}

func (r *txOnlyOrderRepo) Save(_ context.Context, o *order.Order) error {
	r.parent.txSaved = o
	return nil
}

type txAssignRepo struct {
	inserts   []*assignment.ProductAssignment
	current   map[string]*assignment.ProductAssignment
	byID      map[string]*assignment.ProductAssignment
	insertErr error
}

func (r *txAssignRepo) WithTx(*sql.Tx) assignment.Repository { return r }

func (r *txAssignRepo) Insert(_ context.Context, a *assignment.ProductAssignment) error {
	if r.insertErr != nil {
		return r.insertErr
	}
	r.inserts = append(r.inserts, a)
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
	return nil
}

func (r *txAssignRepo) Update(_ context.Context, a *assignment.ProductAssignment) error {
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
	return nil
}

func (r *txAssignRepo) FindByID(_ context.Context, id string) (*assignment.ProductAssignment, error) {
	if r.byID == nil {
		return nil, nil
	}
	a := r.byID[id]
	if a == nil {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}

func (r *txAssignRepo) FindCurrentByChildID(_ context.Context, child string) (*assignment.ProductAssignment, error) {
	if r.current == nil {
		return nil, nil
	}
	a := r.current[child]
	if a == nil {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}

func (r *txAssignRepo) ListByChildID(context.Context, string) ([]assignment.ProductAssignment, error) {
	return nil, nil
}

type assignmentEligMap struct {
	ok map[string]bool
}

func (s assignmentEligMap) IsEligible(_ context.Context, childID, parentID string) (bool, error) {
	return s.ok[childID+"|"+parentID], nil
}

type assignmentPaymentStub struct {
	byOrder map[string]*payment.Payment
}

func (s assignmentPaymentStub) FindByOrderID(_ context.Context, orderID string) (*payment.Payment, error) {
	if s.byOrder == nil {
		return nil, nil
	}
	return s.byOrder[orderID], nil
}

func linkedTicketCatalog() (*mockProductRepo047, *mockVariantRepo037) {
	return &mockProductRepo047{products: map[string]*catalog.Product{
		"prod-1": {
			ID: "prod-1", Status: catalog.StatusActive, Type: catalog.TypeSimple,
			RequiresLinkedParent: true,
		},
	}}, &mockVariantRepo037{variants: variantMap037("v1")}
}

func TestCreateOrderStep_AssignmentRequiresSharedTx(t *testing.T) {
	products, variants := linkedTicketCatalog()
	orders := &mockOrderRepo{}
	credits := &restoreOnlyStoreCredit{mockStoreCreditService: mockStoreCreditService{balance: 1500}}
	assignRepo := &txAssignRepo{}
	flow := assignmentapp.NewFlow(
		assignmentapp.NewService(assignRepo), assignRepo, assignmentEligMap{},
		orders, variants, assignmentPaymentStub{},
	)
	step := checkout.NewCreateOrderStep(orders, variants, credits, nil, products, idleComponents(), idlePrices()).
		WithAssignmentFlow(flow, nil)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Input.StoreCreditAmount = 1000
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.Cart.SetItemAssignment("v1", "", "", true)
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))

	err := step.Execute(context.Background(), cctx)
	if err == nil || !strings.Contains(err.Error(), "shared transaction") {
		t.Fatalf("err = %v, want shared transaction", err)
	}
	if orders.saved != nil {
		t.Fatal("order was saved without a shared transaction")
	}
	if len(assignRepo.inserts) != 0 {
		t.Fatalf("assignment inserts = %d, want 0", len(assignRepo.inserts))
	}
	if len(credits.redeemed) != 1 || credits.redeemed[0] != 1000 {
		t.Fatalf("redeemed = %v, want [1000]", credits.redeemed)
	}
	if len(credits.issued) != 1 || credits.issued[0] != 1000 {
		t.Fatalf("issued = %v, want [1000]", credits.issued)
	}
}

func TestCreateOrderStep_AssignmentInsertFailRollsBackOrderAndCredit(t *testing.T) {
	products, variants := linkedTicketCatalog()
	orders := &splitOrderRepo{}
	credits := &restoreOnlyStoreCredit{mockStoreCreditService: mockStoreCreditService{balance: 1500}}
	assignRepo := &txAssignRepo{insertErr: errors.New("assignment down")}
	flow := assignmentapp.NewFlow(
		assignmentapp.NewService(assignRepo), assignRepo, assignmentEligMap{},
		&bindableOrderRepo{}, variants, assignmentPaymentStub{},
	)
	step := checkout.NewCreateOrderStep(orders, variants, credits, nil, products, idleComponents(), idlePrices()).
		WithAssignmentFlow(flow, assignmentTestDB())

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Input.StoreCreditAmount = 1000
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.Cart.SetItemAssignment("v1", "", "", true)
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))

	err := step.Execute(context.Background(), cctx)
	if err == nil || !strings.Contains(err.Error(), "assignment down") {
		t.Fatalf("err = %v, want assignment insert failure", err)
	}
	if orders.saved != nil {
		t.Fatal("base order repo saved after assignment insert failed")
	}
	if len(credits.issued) != 1 || credits.issued[0] != 1000 {
		t.Fatalf("issued = %v, want credit restored", credits.issued)
	}
}

func TestCreateOrderStep_ExplodedLinkedLeafRecordsAssignment(t *testing.T) {
	products, variants, components, prices := explodeBundleCatalog(t, true, catalog.PricingModeDynamic, catalog.StockModeDynamic)
	products.products["leaf-a"].RequiresLinkedParent = true
	orders := &bindableOrderRepo{}
	assignRepo := &txAssignRepo{}
	elig := assignmentEligMap{ok: map[string]bool{"leaf-a|card-prod": true}}
	flow := assignmentapp.NewFlow(
		assignmentapp.NewService(assignRepo), assignRepo, elig,
		orders, variants, assignmentPaymentStub{},
	)
	step := checkout.NewCreateOrderStep(orders, variants, nil, nil, products, components, prices).
		WithAssignmentFlow(flow, assignmentTestDB())

	cctx := explodeCheckout(t, "vb", 1, 1000)
	cctx.Cart.SetItemAssignment("vb", "card-prod", "", false)
	cctx.SetMeta("reserved", true)
	cctx.SetMeta("reservations", []string{"res-va"})
	cctx.SetMeta(checkout.ReservedVariantQtyMeta, map[string]int{"va": 2})

	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(assignRepo.inserts) != 1 {
		t.Fatalf("inserts = %d, want 1 leaf assignment", len(assignRepo.inserts))
	}
	child := assignRepo.inserts[0].ChildOrderItemID
	if child != assignment.UnitID(cctx.Order.ID, "va") {
		t.Fatalf("child = %q, want leaf unit", child)
	}
	if assignRepo.inserts[0].Status() != assignment.StatusUnassigned {
		t.Fatalf("status = %s, want unassigned", assignRepo.inserts[0].Status())
	}
}

func TestCreateOrderStep_NonExplodedBundleUsesLeafEligibility(t *testing.T) {
	products, variants, components, prices := explodeBundleCatalog(t, false, catalog.PricingModeFixed, catalog.StockModeFixed)
	products.products["leaf-a"].RequiresLinkedParent = true
	orders := &bindableOrderRepo{}
	assignRepo := &txAssignRepo{}
	elig := assignmentEligMap{ok: map[string]bool{"leaf-a|card-prod": true}}
	flow := assignmentapp.NewFlow(
		assignmentapp.NewService(assignRepo), assignRepo, elig,
		orders, variants, assignmentPaymentStub{},
	)
	step := checkout.NewCreateOrderStep(orders, variants, nil, nil, products, components, prices).
		WithAssignmentFlow(flow, assignmentTestDB())

	cctx := explodeCheckout(t, "vb", 1, 1000)
	cctx.Cart.SetItemAssignment("vb", "card-prod", "", false)

	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(assignRepo.inserts) != 1 {
		t.Fatalf("inserts = %d, want 1 bundle unit", len(assignRepo.inserts))
	}
	if assignRepo.inserts[0].ChildOrderItemID != assignment.UnitID(cctx.Order.ID, "vb") {
		t.Fatalf("child = %q, want bundle unit", assignRepo.inserts[0].ChildOrderItemID)
	}
}

func TestCreateOrderStep_NonExplodedBundleRejectsIneligibleLeaf(t *testing.T) {
	products, variants, components, prices := explodeBundleCatalog(t, false, catalog.PricingModeFixed, catalog.StockModeFixed)
	products.products["leaf-a"].RequiresLinkedParent = true
	orders := &splitOrderRepo{}
	assignRepo := &txAssignRepo{}
	flow := assignmentapp.NewFlow(
		assignmentapp.NewService(assignRepo), assignRepo, assignmentEligMap{ok: map[string]bool{"bundle|card-prod": true}},
		&bindableOrderRepo{}, variants, assignmentPaymentStub{},
	)
	step := checkout.NewCreateOrderStep(orders, variants, nil, nil, products, components, prices).
		WithAssignmentFlow(flow, assignmentTestDB())

	cctx := explodeCheckout(t, "vb", 1, 1000)
	cctx.Cart.SetItemAssignment("vb", "card-prod", "", false)

	err := step.Execute(context.Background(), cctx)
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), assignmentapp.ParentNotEligibleMessage) {
		t.Fatalf("err = %v, want leaf eligibility failure", err)
	}
	if orders.saved != nil {
		t.Fatal("order saved after ineligible leaf")
	}
}

func TestInitiatePaymentStep_DeclinedRevokesAssignmentAndDropsParent(t *testing.T) {
	products, variants := linkedTicketCatalog()
	orders := &bindableOrderRepo{}
	assignRepo := &txAssignRepo{}
	payments := assignmentPaymentStub{}
	flow := assignmentapp.NewFlow(
		assignmentapp.NewService(assignRepo), assignRepo, assignmentEligMap{ok: map[string]bool{"other|prod-1": true}},
		orders, variants, payments,
	)
	create := checkout.NewCreateOrderStep(orders, variants, nil, nil, products, idleComponents(), idlePrices()).
		WithAssignmentFlow(flow, assignmentTestDB())

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.Cart.SetItemAssignment("v1", "", "", true)
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))
	if err := create.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("create_order: %v", err)
	}
	if len(assignRepo.inserts) != 1 || !assignRepo.inserts[0].Status().IsCurrent() {
		t.Fatalf("inserts = %+v, want current assignment", assignRepo.inserts)
	}

	payStep := checkout.NewInitiatePaymentStep(
		paymentRegistryWith(&mockPaymentProvider047{
			method: payment.MethodManual,
			result: payment.ProviderResult{Success: false},
		}),
		&mockPaymentRepo047{},
	).WithAssignmentFlow(flow)

	err := payStep.Execute(context.Background(), cctx)
	if err == nil || !strings.Contains(err.Error(), "payment declined") {
		t.Fatalf("err = %v, want declined", err)
	}

	childUnit := assignment.UnitID(cctx.Order.ID, "v1")
	current, err := assignmentapp.NewService(assignRepo).CurrentByChild(context.Background(), childUnit)
	if err != nil {
		t.Fatalf("CurrentByChild: %v", err)
	}
	if current != nil {
		t.Fatalf("current = %+v, want revoked", current)
	}

	parents, err := flow.ListOwnedParents(context.Background(), "cust-1", "other")
	if err != nil {
		t.Fatalf("ListOwnedParents: %v", err)
	}
	if len(parents) != 0 {
		t.Fatalf("parents = %+v, want none for an unpaid decline", parents)
	}
}

func TestCreateOrderStep_ExplodedLinkedLeafIgnoresBundleProduct(t *testing.T) {
	products, variants, components, prices := explodeBundleCatalog(t, true, catalog.PricingModeDynamic, catalog.StockModeDynamic)
	products.products["leaf-a"].RequiresLinkedParent = true
	orders := &splitOrderRepo{}
	assignRepo := &txAssignRepo{}
	flow := assignmentapp.NewFlow(
		assignmentapp.NewService(assignRepo), assignRepo,
		assignmentEligMap{ok: map[string]bool{"bundle|card-prod": true}},
		&bindableOrderRepo{}, variants, assignmentPaymentStub{},
	)
	step := checkout.NewCreateOrderStep(orders, variants, nil, nil, products, components, prices).
		WithAssignmentFlow(flow, assignmentTestDB())

	cctx := explodeCheckout(t, "vb", 1, 1000)
	cctx.Cart.SetItemAssignment("vb", "card-prod", "", false)
	cctx.SetMeta("reserved", true)
	cctx.SetMeta("reservations", []string{"res-va"})
	cctx.SetMeta(checkout.ReservedVariantQtyMeta, map[string]int{"va": 2})

	err := step.Execute(context.Background(), cctx)
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), assignmentapp.ParentNotEligibleMessage) {
		t.Fatalf("err = %v, want leaf eligibility, not bundle", err)
	}
}

func TestWorkflow_UnpaidAfterCreateRevokesAndFailsOrder(t *testing.T) {
	products, variants := linkedTicketCatalog()
	orders := &bindableOrderRepo{}
	assignRepo := &txAssignRepo{}
	payments := assignmentPaymentStub{}
	flow := assignmentapp.NewFlow(
		assignmentapp.NewService(assignRepo), assignRepo, assignmentEligMap{ok: map[string]bool{"other|prod-1": true}},
		orders, variants, payments,
	)
	create := checkout.NewCreateOrderStep(orders, variants, nil, nil, products, idleComponents(), idlePrices()).
		WithAssignmentFlow(flow, assignmentTestDB())
	fail := &mockStep{name: "select_shipping", fn: func(*checkout.Context) error {
		return apperror.Validation("selected shipping method is unavailable")
	}}
	wf := checkout.NewWorkflow([]checkout.Step{create, fail}, testBus(t), testLogger()).
		WithUnpaidOrderCompensation(orders, flow, payments, nil)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.Cart.SetItemAssignment("v1", "", "", true)
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))

	err := wf.Execute(context.Background(), cctx)
	if err == nil {
		t.Fatal("expected shipping failure")
	}
	if cctx.Order == nil || cctx.Order.Status() != order.OrderStatusFailed {
		t.Fatalf("order status = %v, want failed", cctx.Order)
	}
	current, err := assignmentapp.NewService(assignRepo).CurrentByChild(context.Background(), assignment.UnitID(cctx.Order.ID, "v1"))
	if err != nil {
		t.Fatalf("CurrentByChild: %v", err)
	}
	if current != nil {
		t.Fatalf("current = %+v, want revoked", current)
	}
	parents, err := flow.ListOwnedParents(context.Background(), "cust-1", "other")
	if err != nil {
		t.Fatalf("ListOwnedParents: %v", err)
	}
	if len(parents) != 0 {
		t.Fatalf("parents = %+v, want none after unpaid fail", parents)
	}
}

func TestWorkflow_UnpaidAfterCreateRefundsStoreCredit(t *testing.T) {
	products, variants := linkedTicketCatalog()
	orders := &bindableOrderRepo{}
	assignRepo := &txAssignRepo{}
	credits := &mockStoreCreditService{balance: 1500}
	flow := assignmentapp.NewFlow(
		assignmentapp.NewService(assignRepo), assignRepo, assignmentEligMap{},
		orders, variants, assignmentPaymentStub{},
	)
	create := checkout.NewCreateOrderStep(orders, variants, credits, nil, products, idleComponents(), idlePrices()).
		WithAssignmentFlow(flow, assignmentTestDB())
	fail := &mockStep{name: "select_shipping", fn: func(*checkout.Context) error {
		return apperror.Validation("selected shipping method is unavailable")
	}}
	wf := checkout.NewWorkflow([]checkout.Step{create, fail}, testBus(t), testLogger()).
		WithUnpaidOrderCompensation(orders, flow, assignmentPaymentStub{}, credits)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Input.StoreCreditAmount = 1000
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.Cart.SetItemAssignment("v1", "", "", true)
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))

	if err := wf.Execute(context.Background(), cctx); err == nil {
		t.Fatal("expected shipping failure")
	}
	if cctx.Order == nil || cctx.Order.Status() != order.OrderStatusFailed {
		t.Fatalf("order status = %v, want failed", cctx.Order)
	}
	if len(credits.redeemed) != 1 || credits.redeemed[0] != 1000 {
		t.Fatalf("redeemed = %v, want [1000]", credits.redeemed)
	}
	if len(credits.issued) != 1 || credits.issued[0] != 1000 {
		t.Fatalf("issued = %v, want [1000] after compensate", credits.issued)
	}
	if credits.balance != 1500 {
		t.Fatalf("balance = %d, want 1500 restored", credits.balance)
	}
}

func TestWorkflow_PayableZeroShippingFailIsNotOwned(t *testing.T) {
	products, variants := linkedTicketCatalog()
	orders := &bindableOrderRepo{}
	assignRepo := &txAssignRepo{}
	flow := assignmentapp.NewFlow(
		assignmentapp.NewService(assignRepo), assignRepo, assignmentEligMap{ok: map[string]bool{"other|prod-1": true}},
		orders, variants, assignmentPaymentStub{},
	)
	create := checkout.NewCreateOrderStep(orders, variants, nil, nil, products, idleComponents(), idlePrices()).
		WithAssignmentFlow(flow, assignmentTestDB())
	fail := &mockStep{name: "select_shipping", fn: func(*checkout.Context) error {
		return apperror.Validation("selected shipping method is unavailable")
	}}
	wf := checkout.NewWorkflow([]checkout.Step{create, fail}, testBus(t), testLogger()).
		WithUnpaidOrderCompensation(orders, flow, assignmentPaymentStub{}, nil)

	cctx := zeroPriceTicketCheckout(t)
	if err := wf.Execute(context.Background(), cctx); err == nil {
		t.Fatal("expected shipping failure")
	}
	if cctx.Order == nil || cctx.Order.Status() != order.OrderStatusFailed {
		t.Fatalf("order status = %v, want failed", cctx.Order)
	}
	parents, err := flow.ListOwnedParents(context.Background(), "cust-1", "other")
	if err != nil {
		t.Fatalf("ListOwnedParents: %v", err)
	}
	if len(parents) != 0 {
		t.Fatalf("parents = %+v, want none for failed payable-0 order", parents)
	}
}

func TestWorkflow_CompletedPaymentSkipsCompensation(t *testing.T) {
	orders := &bindableOrderRepo{}
	assignRepo := &txAssignRepo{}
	credits := &mockStoreCreditService{balance: 500}
	o := mustCheckoutOrder(t, "cust-1", "v1", 1000)
	if err := o.ApplyStoreCredit(shared.MustNewMoney(500, "EUR")); err != nil {
		t.Fatalf("ApplyStoreCredit: %v", err)
	}
	orders.saved = o
	flow := assignmentapp.NewFlow(
		assignmentapp.NewService(assignRepo), assignRepo, assignmentEligMap{},
		orders, &mockVariantRepo037{variants: variantMap037("v1")}, assignmentPaymentStub{},
	)
	if err := flow.RecordPurchase(context.Background(), "cust-1", o.ID, []assignmentapp.PurchaseLine{{
		ChildVariantID: "v1",
		ChildProductID: "prod-1",
		Assignment:     catalog.AssignmentContext{Deferred: true},
	}}); err != nil {
		t.Fatalf("RecordPurchase: %v", err)
	}
	py, err := payment.NewPayment(id.New(), o.ID, payment.MethodManual, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("NewPayment: %v", err)
	}
	if err := py.Complete("manual:ok"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	fail := &mockStep{name: "after_pay", fn: func(*checkout.Context) error {
		return errors.New("later step")
	}}
	wf := checkout.NewWorkflow([]checkout.Step{fail}, testBus(t), testLogger()).
		WithUnpaidOrderCompensation(orders, flow, assignmentPaymentStub{}, credits)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Order = o
	cctx.SetMeta("created_order_id", o.ID)
	cctx.SetMeta("payment", &py)
	if err := wf.Execute(context.Background(), cctx); err == nil {
		t.Fatal("expected later step failure")
	}
	if o.Status() != order.OrderStatusPending {
		t.Fatalf("status = %s, want pending (payment completed)", o.Status())
	}
	current, err := assignmentapp.NewService(assignRepo).CurrentByChild(context.Background(), assignment.UnitID(o.ID, "v1"))
	if err != nil {
		t.Fatalf("CurrentByChild: %v", err)
	}
	if current == nil || !current.Status().IsCurrent() {
		t.Fatalf("current = %+v, want still current", current)
	}
	if len(credits.issued) != 0 {
		t.Fatalf("issued = %v, want no credit refund after completed payment", credits.issued)
	}
}

func TestWorkflow_ConfirmedOrderSkipsCompensation(t *testing.T) {
	orders := &bindableOrderRepo{}
	assignRepo := &txAssignRepo{}
	credits := &restoreOnlyStoreCredit{}
	o := mustCheckoutOrder(t, "cust-1", "v1", 1000)
	if err := o.ApplyStoreCredit(shared.MustNewMoney(500, "EUR")); err != nil {
		t.Fatalf("ApplyStoreCredit: %v", err)
	}
	if err := o.Confirm(); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	orders.saved = o
	flow := assignmentapp.NewFlow(
		assignmentapp.NewService(assignRepo), assignRepo, assignmentEligMap{},
		orders, &mockVariantRepo037{variants: variantMap037("v1")}, assignmentPaymentStub{},
	)
	if err := flow.RecordPurchase(context.Background(), "cust-1", o.ID, []assignmentapp.PurchaseLine{{
		ChildVariantID: "v1",
		ChildProductID: "prod-1",
		Assignment:     catalog.AssignmentContext{Deferred: true},
	}}); err != nil {
		t.Fatalf("RecordPurchase: %v", err)
	}
	fail := &mockStep{name: "select_shipping", fn: func(*checkout.Context) error {
		return apperror.Validation("selected shipping method is unavailable")
	}}
	wf := checkout.NewWorkflow([]checkout.Step{fail}, testBus(t), testLogger()).
		WithUnpaidOrderCompensation(orders, flow, assignmentPaymentStub{}, credits)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Order = o
	cctx.SetMeta("created_order_id", o.ID)
	if err := wf.Execute(context.Background(), cctx); err == nil {
		t.Fatal("expected shipping failure")
	}
	if o.Status() != order.OrderStatusConfirmed {
		t.Fatalf("status = %s, want confirmed", o.Status())
	}
	if len(credits.issued) != 0 {
		t.Fatalf("issued = %v, want no refund after confirm", credits.issued)
	}
	current, err := assignmentapp.NewService(assignRepo).CurrentByChild(context.Background(), assignment.UnitID(o.ID, "v1"))
	if err != nil {
		t.Fatalf("CurrentByChild: %v", err)
	}
	if current == nil || !current.Status().IsCurrent() {
		t.Fatalf("current = %+v, want still current", current)
	}
}

func TestWorkflow_AlreadyFailedRestoresCredit(t *testing.T) {
	orders := &bindableOrderRepo{}
	credits := &restoreOnlyStoreCredit{}
	o := mustCheckoutOrder(t, "cust-1", "v1", 200000)
	if err := o.ApplyStoreCredit(shared.MustNewMoney(150000, "EUR")); err != nil {
		t.Fatalf("ApplyStoreCredit: %v", err)
	}
	if err := o.Fail(); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	orders.saved = o
	flow := assignmentapp.NewFlow(
		assignmentapp.NewService(&txAssignRepo{}), &txAssignRepo{}, assignmentEligMap{},
		orders, &mockVariantRepo037{variants: variantMap037("v1")}, assignmentPaymentStub{},
	)
	fail := &mockStep{name: "select_shipping", fn: func(*checkout.Context) error {
		return apperror.Validation("selected shipping method is unavailable")
	}}
	wf := checkout.NewWorkflow([]checkout.Step{fail}, testBus(t), testLogger()).
		WithUnpaidOrderCompensation(orders, flow, assignmentPaymentStub{}, credits)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Order = o
	cctx.SetMeta("created_order_id", o.ID)
	if err := wf.Execute(context.Background(), cctx); err == nil {
		t.Fatal("expected shipping failure")
	}
	if len(credits.issued) != 1 || credits.issued[0] != 150000 {
		t.Fatalf("issued = %v, want restore on already-failed order", credits.issued)
	}
}

func TestWorkflow_SyncCompleteUpdateStatusErrorDoesNotRefund(t *testing.T) {
	products, variants := linkedTicketCatalog()
	orders := &bindableOrderRepo{}
	assignRepo := &txAssignRepo{}
	credits := &restoreOnlyStoreCredit{mockStoreCreditService: mockStoreCreditService{balance: 1500}}
	flow := assignmentapp.NewFlow(
		assignmentapp.NewService(assignRepo), assignRepo, assignmentEligMap{},
		orders, variants, assignmentPaymentStub{},
	)
	create := checkout.NewCreateOrderStep(orders, variants, credits, nil, products, idleComponents(), idlePrices()).
		WithAssignmentFlow(flow, assignmentTestDB())
	pay := checkout.NewInitiatePaymentStep(
		paymentRegistryWith(&mockPaymentProvider047{
			method: payment.MethodManual,
			result: payment.ProviderResult{ProviderRef: "manual:ok", Success: true},
		}),
		&mockPaymentRepo047{updateErr: errors.New("conflict")},
	).WithAssignmentFlow(flow)
	wf := checkout.NewWorkflow([]checkout.Step{create, pay}, testBus(t), testLogger()).
		WithUnpaidOrderCompensation(orders, flow, assignmentPaymentStub{}, credits)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Input.StoreCreditAmount = 1000
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.Cart.SetItemAssignment("v1", "", "", true)
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))

	if err := wf.Execute(context.Background(), cctx); err == nil {
		t.Fatal("expected UpdateStatus failure")
	}
	if cctx.Order == nil || cctx.Order.Status() != order.OrderStatusPending {
		t.Fatalf("order status = %v, want pending after approved charge", cctx.Order)
	}
	if len(credits.issued) != 0 {
		t.Fatalf("issued = %v, want no refund after synchronous approve", credits.issued)
	}
}

func TestWorkflow_UnpaidRefundsAboveIssueCap(t *testing.T) {
	orders := &bindableOrderRepo{}
	assignRepo := &txAssignRepo{}
	credits := &restoreOnlyStoreCredit{}
	o := mustCheckoutOrder(t, "cust-1", "v1", 200000)
	if err := o.ApplyStoreCredit(shared.MustNewMoney(150000, "EUR")); err != nil {
		t.Fatalf("ApplyStoreCredit: %v", err)
	}
	orders.saved = o
	flow := assignmentapp.NewFlow(
		assignmentapp.NewService(assignRepo), assignRepo, assignmentEligMap{},
		orders, &mockVariantRepo037{variants: variantMap037("v1")}, assignmentPaymentStub{},
	)
	if err := flow.RecordPurchase(context.Background(), "cust-1", o.ID, []assignmentapp.PurchaseLine{{
		ChildVariantID: "v1",
		ChildProductID: "prod-1",
		Assignment:     catalog.AssignmentContext{Deferred: true},
	}}); err != nil {
		t.Fatalf("RecordPurchase: %v", err)
	}
	fail := &mockStep{name: "select_shipping", fn: func(*checkout.Context) error {
		return apperror.Validation("selected shipping method is unavailable")
	}}
	wf := checkout.NewWorkflow([]checkout.Step{fail}, testBus(t), testLogger()).
		WithUnpaidOrderCompensation(orders, flow, assignmentPaymentStub{}, credits)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Order = o
	cctx.SetMeta("created_order_id", o.ID)
	if err := wf.Execute(context.Background(), cctx); err == nil {
		t.Fatal("expected shipping failure")
	}
	if o.Status() != order.OrderStatusFailed {
		t.Fatalf("status = %s, want failed", o.Status())
	}
	if len(credits.issued) != 1 || credits.issued[0] != 150000 {
		t.Fatalf("issued = %v, want [150000] above the issuance cap", credits.issued)
	}
}

func TestWorkflow_PendingInitiateUpdateStatusErrorDoesNotRefund(t *testing.T) {
	products, variants := linkedTicketCatalog()
	orders := &bindableOrderRepo{}
	assignRepo := &txAssignRepo{}
	credits := &mockStoreCreditService{balance: 1500}
	flow := assignmentapp.NewFlow(
		assignmentapp.NewService(assignRepo), assignRepo, assignmentEligMap{},
		orders, variants, assignmentPaymentStub{},
	)
	create := checkout.NewCreateOrderStep(orders, variants, credits, nil, products, idleComponents(), idlePrices()).
		WithAssignmentFlow(flow, assignmentTestDB())
	pay := checkout.NewInitiatePaymentStep(
		paymentRegistryWith(&mockPaymentProvider047{
			method: payment.MethodStripe,
			result: payment.ProviderResult{ProviderRef: "pi_accepted", Pending: true, ClientSecret: "sec"},
		}),
		&mockPaymentRepo047{updateErr: errors.New("conflict")},
	).WithAssignmentFlow(flow)
	wf := checkout.NewWorkflow([]checkout.Step{create, pay}, testBus(t), testLogger()).
		WithUnpaidOrderCompensation(orders, flow, assignmentPaymentStub{}, credits)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Input.StoreCreditAmount = 1000
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.Cart.SetItemAssignment("v1", "", "", true)
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))

	if err := wf.Execute(context.Background(), cctx); err == nil {
		t.Fatal("expected UpdateStatus failure")
	}
	if cctx.Order == nil || cctx.Order.Status() != order.OrderStatusPending {
		t.Fatalf("order status = %v, want pending (accepted charge)", cctx.Order)
	}
	if len(credits.issued) != 0 {
		t.Fatalf("issued = %v, want no refund while Stripe holds the PaymentIntent", credits.issued)
	}
	current, err := assignmentapp.NewService(assignRepo).CurrentByChild(context.Background(), assignment.UnitID(cctx.Order.ID, "v1"))
	if err != nil {
		t.Fatalf("CurrentByChild: %v", err)
	}
	if current == nil || !current.Status().IsCurrent() {
		t.Fatalf("current = %+v, want still current", current)
	}
	if v, ok := cctx.GetMeta("payment_initiated"); !ok || v != true {
		t.Fatal("payment_initiated must be set before the persist error")
	}
}

func TestWorkflow_PendingInitiateLaterStepDoesNotRefund(t *testing.T) {
	products, variants := linkedTicketCatalog()
	orders := &bindableOrderRepo{}
	assignRepo := &txAssignRepo{}
	credits := &mockStoreCreditService{balance: 1500}
	flow := assignmentapp.NewFlow(
		assignmentapp.NewService(assignRepo), assignRepo, assignmentEligMap{},
		orders, variants, assignmentPaymentStub{},
	)
	create := checkout.NewCreateOrderStep(orders, variants, credits, nil, products, idleComponents(), idlePrices()).
		WithAssignmentFlow(flow, assignmentTestDB())
	pay := checkout.NewInitiatePaymentStep(
		paymentRegistryWith(&mockPaymentProvider047{
			method: payment.MethodStripe,
			result: payment.ProviderResult{ProviderRef: "pi_accepted", Pending: true, ClientSecret: "sec"},
		}),
		&mockPaymentRepo047{},
	).WithAssignmentFlow(flow)
	fail := &mockStep{name: "plugin_after_pay", fn: func(*checkout.Context) error {
		return errors.New("plugin step after initiate_payment")
	}}
	wf := checkout.NewWorkflow([]checkout.Step{create, pay, fail}, testBus(t), testLogger()).
		WithUnpaidOrderCompensation(orders, flow, assignmentPaymentStub{}, credits)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Input.StoreCreditAmount = 1000
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.Cart.SetItemAssignment("v1", "", "", true)
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))

	if err := wf.Execute(context.Background(), cctx); err == nil {
		t.Fatal("expected later step failure")
	}
	if cctx.Order == nil || cctx.Order.Status() != order.OrderStatusPending {
		t.Fatalf("order status = %v, want pending (accepted charge)", cctx.Order)
	}
	if len(credits.issued) != 0 {
		t.Fatalf("issued = %v, want no refund while payment is accepted pending", credits.issued)
	}
	current, err := assignmentapp.NewService(assignRepo).CurrentByChild(context.Background(), assignment.UnitID(cctx.Order.ID, "v1"))
	if err != nil {
		t.Fatalf("CurrentByChild: %v", err)
	}
	if current == nil || !current.Status().IsCurrent() {
		t.Fatalf("current = %+v, want still current", current)
	}
}

func TestWorkflow_DeclinedPaymentRefundsStoreCredit(t *testing.T) {
	products, variants := linkedTicketCatalog()
	orders := &bindableOrderRepo{}
	assignRepo := &txAssignRepo{}
	credits := &mockStoreCreditService{balance: 1500}
	flow := assignmentapp.NewFlow(
		assignmentapp.NewService(assignRepo), assignRepo, assignmentEligMap{},
		orders, variants, assignmentPaymentStub{},
	)
	create := checkout.NewCreateOrderStep(orders, variants, credits, nil, products, idleComponents(), idlePrices()).
		WithAssignmentFlow(flow, assignmentTestDB())
	pay := checkout.NewInitiatePaymentStep(
		paymentRegistryWith(&mockPaymentProvider047{
			method: payment.MethodManual,
			result: payment.ProviderResult{Success: false},
		}),
		&mockPaymentRepo047{},
	).WithAssignmentFlow(flow)
	wf := checkout.NewWorkflow([]checkout.Step{create, pay}, testBus(t), testLogger()).
		WithUnpaidOrderCompensation(orders, flow, assignmentPaymentStub{}, credits)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	cctx.Input.StoreCreditAmount = 1000
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.Cart.SetItemAssignment("v1", "", "", true)
	cctx.SetMeta("pricing", pricingContext037(t, "v1"))

	if err := wf.Execute(context.Background(), cctx); err == nil {
		t.Fatal("expected decline")
	}
	if cctx.Order == nil || cctx.Order.Status() != order.OrderStatusFailed {
		t.Fatalf("order status = %v, want failed after decline", cctx.Order)
	}
	if len(credits.issued) != 1 || credits.issued[0] != 1000 {
		t.Fatalf("issued = %v, want [1000] after decline", credits.issued)
	}
	current, err := assignmentapp.NewService(assignRepo).CurrentByChild(context.Background(), assignment.UnitID(cctx.Order.ID, "v1"))
	if err != nil {
		t.Fatalf("CurrentByChild: %v", err)
	}
	if current != nil {
		t.Fatalf("current = %+v, want revoked after decline", current)
	}
}

type restoreOnlyStoreCredit struct {
	mockStoreCreditService
}

func (m *restoreOnlyStoreCredit) Issue(context.Context, string, shared.Money, string, string) error {
	return errors.New("Issue applies the issuance cap; restore the debit instead")
}

type failLookupOrders struct{}

func (failLookupOrders) FindByID(context.Context, string) (*order.Order, error) {
	return nil, errors.New("order lookup down")
}

func (failLookupOrders) FindByCustomerID(context.Context, string) ([]order.Order, error) {
	return nil, errors.New("order lookup down")
}

func TestRegisterPaymentFailedCompensation_FailsOrderAndRefundsOnce(t *testing.T) {
	orders := &bindableOrderRepo{}
	assignRepo := &txAssignRepo{}
	credits := &restoreOnlyStoreCredit{}
	o := mustCheckoutOrder(t, "cust-1", "v1", 200000)
	if err := o.ApplyStoreCredit(shared.MustNewMoney(150000, "EUR")); err != nil {
		t.Fatalf("ApplyStoreCredit: %v", err)
	}
	orders.saved = o
	flow := assignmentapp.NewFlow(
		assignmentapp.NewService(assignRepo), assignRepo, assignmentEligMap{},
		orders, &mockVariantRepo037{variants: variantMap037("v1")}, assignmentPaymentStub{},
	)
	if err := flow.RecordPurchase(context.Background(), "cust-1", o.ID, []assignmentapp.PurchaseLine{{
		ChildVariantID: "v1",
		ChildProductID: "prod-1",
		Assignment:     catalog.AssignmentContext{Deferred: true},
	}}); err != nil {
		t.Fatalf("RecordPurchase: %v", err)
	}
	bus := testBus(t)
	checkout.RegisterPaymentFailedCompensation(bus, orders, credits, flow)

	evt := event.New(payment.EventPaymentFailed, "test", payment.PaymentStatusChangedData{OrderID: o.ID})
	if err := bus.Publish(context.Background(), evt); err != nil {
		t.Fatalf("first Publish: %v", err)
	}
	if o.Status() != order.OrderStatusFailed {
		t.Fatalf("status = %s, want failed", o.Status())
	}
	if len(credits.issued) != 1 || credits.issued[0] != 150000 {
		t.Fatalf("issued = %v, want [150000] above the issuance cap", credits.issued)
	}
	current, err := assignmentapp.NewService(assignRepo).CurrentByChild(context.Background(), assignment.UnitID(o.ID, "v1"))
	if err != nil {
		t.Fatalf("CurrentByChild: %v", err)
	}
	if current != nil {
		t.Fatalf("current = %+v, want revoked", current)
	}

	if err := bus.Publish(context.Background(), event.New(payment.EventPaymentFailed, "test", payment.PaymentStatusChangedData{OrderID: o.ID})); err != nil {
		t.Fatalf("retry Publish: %v", err)
	}
	if len(credits.issued) != 1 {
		t.Fatalf("issued = %v, want one credit after retry", credits.issued)
	}
}

func TestRegisterPaymentFailedCompensation_RevokeErrorStillRefunds(t *testing.T) {
	orders := &bindableOrderRepo{}
	assignRepo := &txAssignRepo{}
	credits := &restoreOnlyStoreCredit{}
	o := mustCheckoutOrder(t, "cust-1", "v1", 200000)
	if err := o.ApplyStoreCredit(shared.MustNewMoney(150000, "EUR")); err != nil {
		t.Fatalf("ApplyStoreCredit: %v", err)
	}
	orders.saved = o
	writeFlow := assignmentapp.NewFlow(
		assignmentapp.NewService(assignRepo), assignRepo, assignmentEligMap{},
		orders, &mockVariantRepo037{variants: variantMap037("v1")}, assignmentPaymentStub{},
	)
	if err := writeFlow.RecordPurchase(context.Background(), "cust-1", o.ID, []assignmentapp.PurchaseLine{{
		ChildVariantID: "v1",
		ChildProductID: "prod-1",
		Assignment:     catalog.AssignmentContext{Deferred: true},
	}}); err != nil {
		t.Fatalf("RecordPurchase: %v", err)
	}
	revokeFlow := assignmentapp.NewFlow(
		assignmentapp.NewService(assignRepo), assignRepo, assignmentEligMap{},
		failLookupOrders{}, &mockVariantRepo037{variants: variantMap037("v1")}, assignmentPaymentStub{},
	)
	bus := testBus(t)
	checkout.RegisterPaymentFailedCompensation(bus, orders, credits, revokeFlow)

	err := bus.Publish(context.Background(), event.New(payment.EventPaymentFailed, "test", payment.PaymentStatusChangedData{OrderID: o.ID}))
	if err == nil {
		t.Fatal("expected revoke lookup error")
	}
	if o.Status() != order.OrderStatusFailed {
		t.Fatalf("status = %s, want failed even when revoke errors", o.Status())
	}
	if len(credits.issued) != 1 || credits.issued[0] != 150000 {
		t.Fatalf("issued = %v, want refund before revoke error", credits.issued)
	}
}

func TestCreateOrderStep_SharedExplodedLeafCollapses(t *testing.T) {
	products, variants, components, prices := sharedLeafBundleCatalog(t)
	orders := &bindableOrderRepo{}
	assignRepo := &txAssignRepo{}
	flow := assignmentapp.NewFlow(
		assignmentapp.NewService(assignRepo), assignRepo, assignmentEligMap{},
		orders, variants, assignmentPaymentStub{},
	)
	step := checkout.NewCreateOrderStep(orders, variants, nil, nil, products, components, prices).
		WithAssignmentFlow(flow, assignmentTestDB())

	cctx := twoBundleCheckout(t, true, true)
	if err := step.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(assignRepo.inserts) != 1 {
		t.Fatalf("inserts = %d, want 1 collapsed leaf", len(assignRepo.inserts))
	}
	if assignRepo.inserts[0].ChildOrderItemID != assignment.UnitID(cctx.Order.ID, "va") {
		t.Fatalf("child = %q", assignRepo.inserts[0].ChildOrderItemID)
	}
}

func TestCreateOrderStep_SharedExplodedLeafConflict(t *testing.T) {
	products, variants, components, prices := sharedLeafBundleCatalog(t)
	orders := &mockOrderRepo{}
	assignRepo := &txAssignRepo{}
	flow := assignmentapp.NewFlow(
		assignmentapp.NewService(assignRepo), assignRepo, assignmentEligMap{},
		&bindableOrderRepo{}, variants, assignmentPaymentStub{},
	)
	step := checkout.NewCreateOrderStep(orders, variants, nil, nil, products, components, prices).
		WithAssignmentFlow(flow, assignmentTestDB())

	cctx := twoBundleCheckout(t, true, false)
	cctx.Cart.SetItemAssignment("vb2", "card-prod", "", false)
	err := step.Execute(context.Background(), cctx)
	if !apperror.Is(err, apperror.CodeValidation) || !strings.Contains(err.Error(), checkout.AssignmentConflictMessage) {
		t.Fatalf("err = %v, want assignment conflict", err)
	}
	if orders.saved != nil {
		t.Fatal("order saved after assignment conflict")
	}
	if len(assignRepo.inserts) != 0 {
		t.Fatalf("inserts = %d, want 0", len(assignRepo.inserts))
	}
}

func zeroPriceTicketCheckout(t *testing.T) *checkout.Context {
	t.Helper()
	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	c, err := cart.NewCart("cart-1", "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-1"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}
	if err := c.AddItem("v1", 1, shared.MustNewMoney(0, "EUR")); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	c.SetItemAssignment("v1", "", "", true)
	cctx.Cart = &c
	pctx, err := pricing.NewPricingContext("EUR")
	if err != nil {
		t.Fatalf("NewPricingContext: %v", err)
	}
	pi, err := pricing.NewPricingItem("v1", 1, shared.MustNewMoney(0, "EUR"))
	if err != nil {
		t.Fatalf("NewPricingItem: %v", err)
	}
	pctx.Items = []pricing.PricingItem{pi}
	cctx.SetMeta("pricing", &pctx)
	return cctx
}

func mustCheckoutOrder(t *testing.T, customerID, variantID string, amount int64) *order.Order {
	t.Helper()
	item, err := order.NewItem(variantID, "SKU-1", "Ticket", 1, shared.MustNewMoney(amount, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(id.New(), customerID, "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	return &o
}

func sharedLeafBundleCatalog(t *testing.T) (*mockProductRepo047, *mockVariantRepo037, explodeComponentRepo, explodePriceRepo) {
	t.Helper()
	leaf := explodeProduct("leaf-a", catalog.TypeSimple, catalog.PricingModeFixed, catalog.StockModeFixed, false)
	leaf.RequiresLinkedParent = true
	products := &mockProductRepo047{products: map[string]*catalog.Product{
		"bundle-1": explodeProduct("bundle-1", catalog.TypeBundle, catalog.PricingModeDynamic, catalog.StockModeDynamic, true),
		"bundle-2": explodeProduct("bundle-2", catalog.TypeBundle, catalog.PricingModeDynamic, catalog.StockModeDynamic, true),
		"leaf-a":   leaf,
	}}
	variants := &mockVariantRepo037{variants: map[string]*catalog.Variant{
		"vb1": explodeVariant(t, "vb1", "bundle-1", "Bundle 1"),
		"vb2": explodeVariant(t, "vb2", "bundle-2", "Bundle 2"),
		"va":  explodeVariant(t, "va", "leaf-a", "Leaf A"),
	}}
	components := explodeComponentRepo{rows: []catalog.BundleComponent{
		mustComponent(t, "bundle-1", "leaf-a", 1),
		mustComponent(t, "bundle-2", "leaf-a", 1),
	}}
	prices := explodePriceRepo{byVariant: map[string]int64{"va": 300}}
	return products, variants, components, prices
}

func twoBundleCheckout(t *testing.T, assignFirst, assignSecond bool) *checkout.Context {
	t.Helper()
	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	attachCreateOrderInput(cctx)
	c := mustExplodeCart(t)
	if err := c.AddItem("vb1", 1, shared.MustNewMoney(300, "EUR")); err != nil {
		t.Fatalf("AddItem vb1: %v", err)
	}
	if err := c.AddItem("vb2", 1, shared.MustNewMoney(300, "EUR")); err != nil {
		t.Fatalf("AddItem vb2: %v", err)
	}
	if assignFirst {
		c.SetItemAssignment("vb1", "", "", true)
	}
	if assignSecond {
		c.SetItemAssignment("vb2", "", "", true)
	}
	cctx.Cart = &c
	pctx, err := pricing.NewPricingContext("EUR")
	if err != nil {
		t.Fatalf("NewPricingContext: %v", err)
	}
	for _, vid := range []string{"vb1", "vb2"} {
		pi, err := pricing.NewPricingItem(vid, 1, shared.MustNewMoney(300, "EUR"))
		if err != nil {
			t.Fatalf("NewPricingItem %s: %v", vid, err)
		}
		pctx.Items = append(pctx.Items, pi)
	}
	cctx.SetMeta("pricing", &pctx)
	cctx.SetMeta("reserved", true)
	cctx.SetMeta("reservations", []string{"res-va"})
	cctx.SetMeta(checkout.ReservedVariantQtyMeta, map[string]int{"va": 2})
	return cctx
}
