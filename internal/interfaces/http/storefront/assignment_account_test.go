package storefront_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	assignmentapp "github.com/akarso/shopanda/internal/application/assignment"
	"github.com/akarso/shopanda/internal/domain/assignment"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/shared"
	storefront "github.com/akarso/shopanda/internal/interfaces/http/storefront"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/id"
)

type acctElig struct{ ok bool }

func (s acctElig) IsEligible(context.Context, string, string) (bool, error) { return s.ok, nil }

type acctAssignRepo struct {
	current *assignment.ProductAssignment
}

func (r *acctAssignRepo) Insert(_ context.Context, a *assignment.ProductAssignment) error {
	cp := *a
	r.current = &cp
	return nil
}
func (r *acctAssignRepo) Update(_ context.Context, a *assignment.ProductAssignment) error {
	a.AcceptStoredStatus()
	cp := *a
	r.current = &cp
	return nil
}
func (r *acctAssignRepo) FindByID(_ context.Context, id string) (*assignment.ProductAssignment, error) {
	if r.current == nil || r.current.ID != id {
		return nil, nil
	}
	cp := *r.current
	return &cp, nil
}
func (r *acctAssignRepo) FindCurrentByChildID(_ context.Context, child string) (*assignment.ProductAssignment, error) {
	if r.current == nil || r.current.ChildOrderItemID != child || !r.current.Status().IsCurrent() {
		return nil, nil
	}
	cp := *r.current
	return &cp, nil
}
func (r *acctAssignRepo) ListByChildID(context.Context, string) ([]assignment.ProductAssignment, error) {
	return nil, nil
}

type acctOrders struct {
	byID map[string]*order.Order
}

func (s acctOrders) FindByID(_ context.Context, id string) (*order.Order, error) {
	return s.byID[id], nil
}
func (acctOrders) FindByCustomerID(context.Context, string) ([]order.Order, error) { return nil, nil }

type acctVariants struct {
	byID map[string]*catalog.Variant
}

func (s acctVariants) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	return s.byID[id], nil
}

type acctPayments struct{}

func (acctPayments) FindByOrderID(context.Context, string) (*payment.Payment, error) { return nil, nil }

func TestAssignmentAccountHandler_Assign(t *testing.T) {
	parentItem, err := order.NewItem("card-var", "SKU-C", "Card", 1, shared.MustNewMoney(0, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	childItem, err := order.NewItem("ticket-var", "SKU-T", "Ticket", 1, shared.MustNewMoney(0, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	parentOrder, err := order.NewOrder(id.New(), "cust-1", "", "EUR", []order.Item{parentItem})
	if err != nil {
		t.Fatalf("parent order: %v", err)
	}
	childOrder, err := order.NewOrder(id.New(), "cust-1", "", "EUR", []order.Item{childItem})
	if err != nil {
		t.Fatalf("child order: %v", err)
	}
	parentUnit := assignment.UnitID(parentOrder.ID, "card-var")
	childUnit := assignment.UnitID(childOrder.ID, "ticket-var")
	repo := &acctAssignRepo{}
	flow := assignmentapp.NewFlow(assignmentapp.NewService(repo), repo, acctElig{ok: true}, acctOrders{byID: map[string]*order.Order{
		parentOrder.ID: &parentOrder,
		childOrder.ID:  &childOrder,
	}}, acctVariants{byID: map[string]*catalog.Variant{
		"card-var":   {ID: "card-var", ProductID: "card-prod"},
		"ticket-var": {ID: "ticket-var", ProductID: "ticket-prod"},
	}}, acctPayments{})
	h := storefront.NewAssignmentAccountHandler(flow)
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/account/assignments", h.Assign())

	rec := httptest.NewRecorder()
	body := `{"child_unit_id":"` + childUnit + `","parent_unit_id":"` + parentUnit + `"}`
	req := httptest.NewRequest("POST", "/api/v1/account/assignments", strings.NewReader(body))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var env map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	data := env["data"].(map[string]interface{})
	got := data["assignment"].(map[string]interface{})
	if got["status"] != "assigned" || got["parent_order_item_id"] != parentUnit {
		t.Fatalf("assignment = %+v", got)
	}
}

func TestAssignmentAccountHandler_Assign_RequiresIDs(t *testing.T) {
	repo := &acctAssignRepo{}
	flow := assignmentapp.NewFlow(assignmentapp.NewService(repo), repo, acctElig{ok: true}, acctOrders{}, acctVariants{}, acctPayments{})
	h := storefront.NewAssignmentAccountHandler(flow)
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/account/assignments", h.Assign())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/account/assignments", strings.NewReader(`{}`))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}
