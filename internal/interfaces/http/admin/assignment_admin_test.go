package admin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	assignmentapp "github.com/akarso/shopanda/internal/application/assignment"
	domainadmin "github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/domain/assignment"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/interfaces/http/admin"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type adminAssignRepo struct {
	byID    map[string]*assignment.ProductAssignment
	current map[string]*assignment.ProductAssignment
	all     []assignment.ProductAssignment
}

func (r *adminAssignRepo) remember(a *assignment.ProductAssignment) {
	if r.byID == nil {
		r.byID = map[string]*assignment.ProductAssignment{}
	}
	if r.current == nil {
		r.current = map[string]*assignment.ProductAssignment{}
	}
	cp := *a
	r.byID[a.ID] = &cp
	replaced := false
	for i := range r.all {
		if r.all[i].ID == a.ID {
			r.all[i] = cp
			replaced = true
			break
		}
	}
	if !replaced {
		r.all = append(r.all, cp)
	}
	if a.Status().IsCurrent() {
		cur := cp
		r.current[a.ChildOrderItemID] = &cur
	} else {
		delete(r.current, a.ChildOrderItemID)
	}
}

func (r *adminAssignRepo) Insert(_ context.Context, a *assignment.ProductAssignment) error {
	r.remember(a)
	return nil
}
func (r *adminAssignRepo) Update(_ context.Context, a *assignment.ProductAssignment) error {
	a.AcceptStoredStatus()
	r.remember(a)
	return nil
}
func (r *adminAssignRepo) FindByID(_ context.Context, assignmentID string) (*assignment.ProductAssignment, error) {
	if r.byID == nil {
		return nil, nil
	}
	a := r.byID[assignmentID]
	if a == nil {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}
func (r *adminAssignRepo) FindCurrentByChildID(_ context.Context, child string) (*assignment.ProductAssignment, error) {
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
func (r *adminAssignRepo) ListByChildID(context.Context, string) ([]assignment.ProductAssignment, error) {
	return []assignment.ProductAssignment{}, nil
}
func (r *adminAssignRepo) ListByParentID(_ context.Context, parent string, page assignment.ParentPage) ([]assignment.ProductAssignment, bool, error) {
	out := make([]assignment.ProductAssignment, 0)
	for i := len(r.all) - 1; i >= 0; i-- {
		if r.all[i].ParentOrderItemID == parent {
			out = append(out, r.all[i])
		}
	}
	paged, hasMore := pageAdminAssignments(out, page)
	return paged, hasMore, nil
}

func pageAdminAssignments(rows []assignment.ProductAssignment, page assignment.ParentPage) ([]assignment.ProductAssignment, bool) {
	status := page.Status
	if status == "" {
		status = string(assignment.StatusAssigned)
	}
	limit := page.Limit
	if limit <= 0 {
		limit = 100
	}
	offset := page.Offset
	if offset < 0 {
		offset = 0
	}
	filtered := make([]assignment.ProductAssignment, 0, len(rows))
	for _, row := range rows {
		if status != "all" && string(row.Status()) != status {
			continue
		}
		filtered = append(filtered, row)
	}
	if offset >= len(filtered) {
		return []assignment.ProductAssignment{}, false
	}
	rest := filtered[offset:]
	if len(rest) > limit {
		return rest[:limit], true
	}
	return rest, false
}

type adminAssignOrders struct {
	byID     map[string]*order.Order
	batchErr error
}

func (s adminAssignOrders) FindByID(_ context.Context, orderID string) (*order.Order, error) {
	return s.byID[orderID], nil
}
func (s adminAssignOrders) FindByIDs(_ context.Context, ids []string) (map[string]*order.Order, error) {
	if s.batchErr != nil {
		return nil, s.batchErr
	}
	out := make(map[string]*order.Order, len(ids))
	for _, id := range ids {
		if o := s.byID[id]; o != nil {
			out[id] = o
		}
	}
	return out, nil
}
func (adminAssignOrders) FindByCustomerID(context.Context, string) ([]order.Order, error) {
	return nil, nil
}

type adminAssignVariants struct {
	byID map[string]*catalog.Variant
}

func (s adminAssignVariants) FindByID(_ context.Context, variantID string) (*catalog.Variant, error) {
	return s.byID[variantID], nil
}

type adminAssignElig struct{ ok bool }

func (s adminAssignElig) IsEligible(context.Context, string, string) (bool, error) {
	return s.ok, nil
}

type adminAssignPayments struct {
	byOrder map[string]*payment.Payment
}

func (s adminAssignPayments) FindByOrderID(_ context.Context, orderID string) (*payment.Payment, error) {
	if s.byOrder == nil {
		return nil, nil
	}
	return s.byOrder[orderID], nil
}

type auditCapture struct {
	records []domainadmin.AuditLogRecord
}

func (a *auditCapture) Insert(_ context.Context, record domainadmin.AuditLogRecord) error {
	a.records = append(a.records, record)
	return nil
}
func (a *auditCapture) List(context.Context, domainadmin.AuditLogFilter) ([]domainadmin.AuditLogRecord, error) {
	return nil, nil
}
func (a *auditCapture) DeleteBefore(context.Context, time.Time) (int64, error) { return 0, nil }

type adminAssignProducts struct {
	byID map[string]*catalog.Product
}

func (s adminAssignProducts) FindByID(_ context.Context, productID string) (*catalog.Product, error) {
	if s.byID == nil {
		return nil, nil
	}
	return s.byID[productID], nil
}

type adminAssignComponents struct{}

func (adminAssignComponents) ListByBundleID(context.Context, string) ([]catalog.BundleComponent, error) {
	return nil, nil
}

func newAssignmentAdminMux(t *testing.T, repo *adminAssignRepo, orders adminAssignOrders, variants adminAssignVariants, audits *auditCapture) *http.ServeMux {
	t.Helper()
	return newAssignmentAdminMuxElig(t, repo, orders, variants, audits, true)
}

func newAssignmentAdminMuxElig(t *testing.T, repo *adminAssignRepo, orders adminAssignOrders, variants adminAssignVariants, audits *auditCapture, eligible bool) *http.ServeMux {
	t.Helper()
	return newAssignmentAdminMuxConfigured(t, repo, orders, variants, audits, eligible, nil, adminAssignPayments{})
}

func newAssignmentAdminMuxConfigured(
	t *testing.T,
	repo *adminAssignRepo,
	orders adminAssignOrders,
	variants adminAssignVariants,
	audits *auditCapture,
	eligible bool,
	products map[string]*catalog.Product,
	payments adminAssignPayments,
) *http.ServeMux {
	t.Helper()
	if products == nil {
		products = map[string]*catalog.Product{
			"ticket-prod": {ID: "ticket-prod", Type: catalog.TypeSimple, Status: catalog.StatusActive, RequiresLinkedParent: true},
			"card-prod":   {ID: "card-prod", Type: catalog.TypeSimple, Status: catalog.StatusActive},
		}
	}
	svc := assignmentapp.NewService(repo)
	flow := assignmentapp.NewFlow(svc, repo, adminAssignElig{ok: eligible}, orders, variants, payments).WithLinkedLeaves(
		adminAssignProducts{byID: products},
		adminAssignComponents{},
	)
	auditor := adminapp.NewAuditor(logger.New("error"))
	auditor.SetAuditLogRepository(audits)
	h := admin.NewAssignmentAdminHandler(flow, svc, auditor)
	requireRead := admin.RequirePermission(rbac.AssignmentsRead)
	requireWrite := admin.RequirePermission(rbac.AssignmentsWrite)
	withAdmin := admin.AdminContextMiddleware()
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/assignments", withAdmin(requireRead(h.ListByParent())))
	mux.Handle("GET /api/v1/admin/assignments/current", withAdmin(requireRead(h.GetCurrent())))
	mux.Handle("POST /api/v1/admin/assignments", withAdmin(requireWrite(h.Create())))
	mux.Handle("POST /api/v1/admin/assignments/{id}/revoke", withAdmin(requireWrite(h.Revoke())))
	return mux
}

func TestAssignmentAdminHandler_CreateListCurrentRevoke(t *testing.T) {
	parentItem, err := order.NewItem("card-var", "SKU-C", "Card", 1, shared.MustNewMoney(0, "EUR"))
	if err != nil {
		t.Fatalf("parent item: %v", err)
	}
	childItem, err := order.NewItem("ticket-var", "SKU-T", "Ticket", 1, shared.MustNewMoney(0, "EUR"))
	if err != nil {
		t.Fatalf("child item: %v", err)
	}
	parentOrder, err := order.NewOrder(id.New(), "other", "", "EUR", []order.Item{parentItem})
	if err != nil {
		t.Fatalf("parent order: %v", err)
	}
	childOrder, err := order.NewOrder(id.New(), "cust-1", "", "EUR", []order.Item{childItem})
	if err != nil {
		t.Fatalf("child order: %v", err)
	}
	parentUnit := assignment.UnitID(parentOrder.ID, "card-var")
	childUnit := assignment.UnitID(childOrder.ID, "ticket-var")
	repo := &adminAssignRepo{}
	audits := &auditCapture{}
	mux := newAssignmentAdminMux(t, repo, adminAssignOrders{byID: map[string]*order.Order{
		parentOrder.ID: &parentOrder,
		childOrder.ID:  &childOrder,
	}}, adminAssignVariants{byID: map[string]*catalog.Variant{
		"card-var":   {ID: "card-var", ProductID: "card-prod"},
		"ticket-var": {ID: "ticket-var", ProductID: "ticket-prod"},
	}}, audits)

	body := `{"child_unit_id":"` + childUnit + `","parent_unit_id":"` + parentUnit + `"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/assignments", strings.NewReader(body))
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Data struct {
			Assignment struct {
				ID                string `json:"id"`
				ParentOrderItemID string `json:"parent_order_item_id"`
				ChildOrderItemID  string `json:"child_order_item_id"`
				Status            string `json:"status"`
			} `json:"assignment"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Data.Assignment.Status != "assigned" ||
		created.Data.Assignment.ParentOrderItemID != parentUnit ||
		created.Data.Assignment.ChildOrderItemID != childUnit {
		t.Fatalf("created = %+v", created.Data.Assignment)
	}
	if len(audits.records) != 1 || audits.records[0].Action != "assignment.create" || audits.records[0].Result != "success" {
		t.Fatalf("audit after create = %+v", audits.records)
	}

	q := url.Values{}
	q.Set("child_unit_id", " "+childUnit+" ")
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/assignments/current?"+q.Encode(), nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), created.Data.Assignment.ID) {
		t.Fatalf("current status=%d body=%s", rec.Code, rec.Body.String())
	}

	q = url.Values{}
	q.Set("parent_unit_id", parentUnit)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/assignments?"+q.Encode(), nil)
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), childUnit) {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/assignments", strings.NewReader(body))
	req = testhelper.AuthenticatedRequest(req, "editor-1", identity.RoleEditor)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor create status = %d, want 403 body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/assignments", strings.NewReader(body))
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("already assigned status = %d, want 409 body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/assignments/"+created.Data.Assignment.ID+"/revoke", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"revoked"`) ||
		!strings.Contains(rec.Body.String(), `"child_customer_id":"cust-1"`) ||
		!strings.Contains(rec.Body.String(), `"parent_customer_id":"other"`) {
		t.Fatalf("revoke status=%d body=%s", rec.Code, rec.Body.String())
	}
	if audits.records[len(audits.records)-1].Action != "assignment.revoke" || audits.records[len(audits.records)-1].ResourceID != created.Data.Assignment.ID {
		t.Fatalf("audit after revoke = %+v", audits.records)
	}

	q = url.Values{}
	q.Set("child_unit_id", childUnit)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/assignments/current?"+q.Encode(), nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"assignment":null`) {
		t.Fatalf("current after revoke status=%d body=%s", rec.Code, rec.Body.String())
	}

	q = url.Values{}
	q.Set("parent_unit_id", parentUnit)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/assignments?"+q.Encode(), nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"status":"revoked"`) {
		t.Fatalf("default list after revoke status=%d body=%s", rec.Code, rec.Body.String())
	}
	q.Set("status", "all")
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/assignments?"+q.Encode(), nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"revoked"`) {
		t.Fatalf("list all after revoke status=%d body=%s", rec.Code, rec.Body.String())
	}

	before := len(audits.records)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/assignments/"+created.Data.Assignment.ID+"/revoke", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"revoked"`) {
		t.Fatalf("idempotent revoke status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(audits.records) != before {
		t.Fatalf("idempotent revoke audited %d new records", len(audits.records)-before)
	}
}

func TestAssignmentAdminHandler_MissingQuery(t *testing.T) {
	mux := newAssignmentAdminMux(t, &adminAssignRepo{}, adminAssignOrders{}, adminAssignVariants{}, &auditCapture{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/assignments", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("list status = %d, want 422 body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/assignments/current", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("current status = %d, want 422 body=%s", rec.Code, rec.Body.String())
	}
}

func assignmentAdminFixture(t *testing.T) (parentUnit, childUnit string, orders adminAssignOrders, variants adminAssignVariants) {
	t.Helper()
	parentItem, err := order.NewItem("card-var", "SKU-C", "Card", 1, shared.MustNewMoney(0, "EUR"))
	if err != nil {
		t.Fatalf("parent item: %v", err)
	}
	childItem, err := order.NewItem("ticket-var", "SKU-T", "Ticket", 1, shared.MustNewMoney(0, "EUR"))
	if err != nil {
		t.Fatalf("child item: %v", err)
	}
	parentOrder, err := order.NewOrder(id.New(), "other", "", "EUR", []order.Item{parentItem})
	if err != nil {
		t.Fatalf("parent order: %v", err)
	}
	childOrder, err := order.NewOrder(id.New(), "cust-1", "", "EUR", []order.Item{childItem})
	if err != nil {
		t.Fatalf("child order: %v", err)
	}
	return assignment.UnitID(parentOrder.ID, "card-var"), assignment.UnitID(childOrder.ID, "ticket-var"), adminAssignOrders{byID: map[string]*order.Order{
			parentOrder.ID: &parentOrder,
			childOrder.ID:  &childOrder,
		}}, adminAssignVariants{byID: map[string]*catalog.Variant{
			"card-var":   {ID: "card-var", ProductID: "card-prod"},
			"ticket-var": {ID: "ticket-var", ProductID: "ticket-prod"},
		}}
}

func TestAssignmentAdminHandler_ValidationAndAudit(t *testing.T) {
	parentUnit, childUnit, orders, variants := assignmentAdminFixture(t)
	repo := &adminAssignRepo{}
	audits := &auditCapture{}
	mux := newAssignmentAdminMuxElig(t, repo, orders, variants, audits, false)

	body := `{"child_unit_id":"` + childUnit + `","parent_unit_id":"` + parentUnit + `"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/assignments", strings.NewReader(body))
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("ineligible status = %d body=%s", rec.Code, rec.Body.String())
	}

	missing := assignment.UnitID(id.New(), "ticket-var")
	body = `{"child_unit_id":"` + missing + `","parent_unit_id":"` + parentUnit + `"}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/assignments", strings.NewReader(body))
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing unit status = %d body=%s", rec.Code, rec.Body.String())
	}

	body = `{"child_unit_id":"foo/bar","parent_unit_id":"` + parentUnit + `"}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/assignments", strings.NewReader(body))
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("foo/bar status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(audits.records) == 0 || audits.records[len(audits.records)-1].ResourceID != "foo/bar" || audits.records[len(audits.records)-1].Result != "error" {
		t.Fatalf("failed create audit = %+v", audits.records)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/assignments/"+id.New()+"/revoke", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("revoke missing status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAssignmentAdminHandler_SupportWriteEditorForbidden(t *testing.T) {
	parentUnit, childUnit, orders, variants := assignmentAdminFixture(t)
	repo := &adminAssignRepo{}
	audits := &auditCapture{}
	mux := newAssignmentAdminMux(t, repo, orders, variants, audits)
	svc := assignmentapp.NewService(repo)
	if _, err := svc.CreateUnassigned(context.Background(), childUnit); err != nil {
		t.Fatalf("CreateUnassigned: %v", err)
	}

	body := `{"child_unit_id":"` + childUnit + `","parent_unit_id":"` + parentUnit + `"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/assignments", strings.NewReader(body))
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("support assign existing row status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		Data struct {
			Assignment struct {
				ID string `json:"id"`
			} `json:"assignment"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/assignments/"+created.Data.Assignment.ID+"/revoke", nil)
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("support revoke status = %d body=%s", rec.Code, rec.Body.String())
	}

	q := url.Values{}
	q.Set("parent_unit_id", parentUnit)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/assignments?"+q.Encode(), nil)
	req = testhelper.AuthenticatedRequest(req, "editor-1", identity.RoleEditor)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor list status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/assignments", strings.NewReader(body))
	req = testhelper.AuthenticatedRequest(req, "editor-1", identity.RoleEditor)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor create status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/assignments/"+created.Data.Assignment.ID+"/revoke", nil)
	req = testhelper.AuthenticatedRequest(req, "editor-1", identity.RoleEditor)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor revoke status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAssignmentAdminHandler_StatusMatrix(t *testing.T) {
	parentItem, err := order.NewItem("card-var", "SKU-C", "Card", 1, shared.MustNewMoney(0, "EUR"))
	if err != nil {
		t.Fatalf("parent item: %v", err)
	}
	linkedItem, err := order.NewItem("ticket-var", "SKU-T", "Ticket", 1, shared.MustNewMoney(0, "EUR"))
	if err != nil {
		t.Fatalf("linked item: %v", err)
	}
	plainItem, err := order.NewItem("plain-var", "SKU-P", "Plain", 1, shared.MustNewMoney(0, "EUR"))
	if err != nil {
		t.Fatalf("plain item: %v", err)
	}
	unpaidItem, err := order.NewItem("unpaid-var", "SKU-U", "Unpaid", 1, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("unpaid item: %v", err)
	}
	cancelledItem, err := order.NewItem("cancel-var", "SKU-X", "Cancelled", 1, shared.MustNewMoney(0, "EUR"))
	if err != nil {
		t.Fatalf("cancelled item: %v", err)
	}
	unpaidParentItem, err := order.NewItem("unpaid-card-var", "SKU-UP", "Unpaid card", 1, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("unpaid parent item: %v", err)
	}
	missingItem, err := order.NewItem("gone-var", "SKU-G", "Gone", 1, shared.MustNewMoney(0, "EUR"))
	if err != nil {
		t.Fatalf("missing item: %v", err)
	}
	parentOrder, err := order.NewOrder(id.New(), "other", "", "EUR", []order.Item{parentItem})
	if err != nil {
		t.Fatalf("parent order: %v", err)
	}
	linkedOrder, err := order.NewOrder(id.New(), "cust-1", "", "EUR", []order.Item{linkedItem})
	if err != nil {
		t.Fatalf("linked order: %v", err)
	}
	plainOrder, err := order.NewOrder(id.New(), "cust-1", "", "EUR", []order.Item{plainItem})
	if err != nil {
		t.Fatalf("plain order: %v", err)
	}
	unpaidOrder, err := order.NewOrder(id.New(), "cust-1", "", "EUR", []order.Item{unpaidItem})
	if err != nil {
		t.Fatalf("unpaid order: %v", err)
	}
	cancelledOrder, err := order.NewOrder(id.New(), "cust-1", "", "EUR", []order.Item{cancelledItem})
	if err != nil {
		t.Fatalf("cancelled order: %v", err)
	}
	if err := cancelledOrder.SetStatusFromDB(string(order.OrderStatusCancelled)); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	unpaidParentOrder, err := order.NewOrder(id.New(), "other", "", "EUR", []order.Item{unpaidParentItem})
	if err != nil {
		t.Fatalf("unpaid parent order: %v", err)
	}
	missingOrder, err := order.NewOrder(id.New(), "cust-1", "", "EUR", []order.Item{missingItem})
	if err != nil {
		t.Fatalf("missing order: %v", err)
	}
	parentUnit := assignment.UnitID(parentOrder.ID, "card-var")
	linkedUnit := assignment.UnitID(linkedOrder.ID, "ticket-var")
	plainUnit := assignment.UnitID(plainOrder.ID, "plain-var")
	unpaidUnit := assignment.UnitID(unpaidOrder.ID, "unpaid-var")
	cancelledUnit := assignment.UnitID(cancelledOrder.ID, "cancel-var")
	unpaidParentUnit := assignment.UnitID(unpaidParentOrder.ID, "unpaid-card-var")
	missingUnit := assignment.UnitID(missingOrder.ID, "gone-var")
	repo := &adminAssignRepo{}
	products := map[string]*catalog.Product{
		"ticket-prod": {ID: "ticket-prod", Type: catalog.TypeSimple, Status: catalog.StatusActive, RequiresLinkedParent: true},
		"plain-prod":  {ID: "plain-prod", Type: catalog.TypeSimple, Status: catalog.StatusActive},
		"card-prod":   {ID: "card-prod", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}
	mux := newAssignmentAdminMuxConfigured(t, repo, adminAssignOrders{byID: map[string]*order.Order{
		parentOrder.ID:       &parentOrder,
		linkedOrder.ID:       &linkedOrder,
		plainOrder.ID:        &plainOrder,
		unpaidOrder.ID:       &unpaidOrder,
		cancelledOrder.ID:    &cancelledOrder,
		unpaidParentOrder.ID: &unpaidParentOrder,
		missingOrder.ID:      &missingOrder,
	}}, adminAssignVariants{byID: map[string]*catalog.Variant{
		"card-var":        {ID: "card-var", ProductID: "card-prod"},
		"ticket-var":      {ID: "ticket-var", ProductID: "ticket-prod"},
		"plain-var":       {ID: "plain-var", ProductID: "plain-prod"},
		"unpaid-var":      {ID: "unpaid-var", ProductID: "ticket-prod"},
		"cancel-var":      {ID: "cancel-var", ProductID: "ticket-prod"},
		"unpaid-card-var": {ID: "unpaid-card-var", ProductID: "card-prod"},
	}}, &auditCapture{}, true, products, adminAssignPayments{})

	post := func(body string, role identity.Role) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/assignments", strings.NewReader(body))
		req = testhelper.AuthenticatedRequest(req, "user-1", role)
		mux.ServeHTTP(rec, req)
		return rec
	}
	body := func(child, parent string, force bool, reason string) string {
		return `{"child_unit_id":"` + child + `","parent_unit_id":"` + parent + `","force":` + strconv.FormatBool(force) + `,"reason":"` + reason + `"}`
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/assignments?parent_unit_id=foo/bar", nil)
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("foo/bar list status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = post(body("foo/bar", parentUnit, false, ""), identity.RoleAdmin)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("foo/bar create status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = post(body(missingUnit, parentUnit, false, ""), identity.RoleAdmin)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), assignmentapp.ChildVariantMissingMessage) || strings.Contains(rec.Body.String(), assignmentapp.ChildUnitMissingMessage) {
		t.Fatalf("missing variant status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = post(body(linkedUnit, unpaidParentUnit, false, ""), identity.RoleAdmin)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), assignmentapp.ParentOrderNotBindableMessage) {
		t.Fatalf("unpaid parent status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = post(body(plainUnit, parentUnit, false, ""), identity.RoleAdmin)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), assignmentapp.ChildNotLinkedMessage) {
		t.Fatalf("not linked status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := assignmentapp.NewService(repo).CreateUnassigned(context.Background(), plainUnit); err != nil {
		t.Fatalf("CreateUnassigned: %v", err)
	}
	rec = post(body(plainUnit, parentUnit, false, ""), identity.RoleSupport)
	if rec.Code != http.StatusOK {
		t.Fatalf("unassigned without flag status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = post(body(unpaidUnit, parentUnit, false, ""), identity.RoleAdmin)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), assignmentapp.ChildOrderNotBindableMessage) {
		t.Fatalf("unpaid status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = post(body(unpaidUnit, parentUnit, true, ""), identity.RoleAdmin)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), assignmentapp.ForceReasonRequiredMessage) {
		t.Fatalf("force without reason status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = post(body(unpaidUnit, parentUnit, true, "correct an unpaid ticket"), identity.RoleSupport)
	if rec.Code != http.StatusCreated {
		t.Fatalf("force unpaid status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = post(body(cancelledUnit, parentUnit, false, ""), identity.RoleAdmin)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), assignmentapp.ChildOrderNotBindableMessage) {
		t.Fatalf("cancelled status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = post(body(cancelledUnit, parentUnit, true, "correct a cancelled ticket"), identity.RoleSupport)
	if rec.Code != http.StatusCreated ||
		!strings.Contains(rec.Body.String(), `"child_order_status":"cancelled"`) ||
		!strings.Contains(rec.Body.String(), `"child_customer_id":"cust-1"`) ||
		!strings.Contains(rec.Body.String(), `"forced":true`) ||
		!strings.Contains(rec.Body.String(), `"reason":"correct a cancelled ticket"`) ||
		strings.Contains(rec.Body.String(), `"forced_reason"`) {
		t.Fatalf("force cancelled status = %d body=%s", rec.Code, rec.Body.String())
	}
	q := url.Values{}
	q.Set("child_unit_id", cancelledUnit)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/assignments/current?"+q.Encode(), nil)
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), `"child_order_status":"cancelled"`) ||
		!strings.Contains(rec.Body.String(), `"forced":true`) ||
		!strings.Contains(rec.Body.String(), `"reason":"correct a cancelled ticket"`) ||
		strings.Contains(rec.Body.String(), `"forced_reason"`) {
		t.Fatalf("current after force status=%d body=%s", rec.Code, rec.Body.String())
	}
	q = url.Values{}
	q.Set("parent_unit_id", parentUnit)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/assignments?"+q.Encode(), nil)
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), `"child_order_status":"cancelled"`) ||
		!strings.Contains(rec.Body.String(), `"forced":true`) ||
		!strings.Contains(rec.Body.String(), cancelledUnit) {
		t.Fatalf("list after force status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = post(body(linkedUnit, parentUnit, true, "already bindable"), identity.RoleSupport)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), assignmentapp.ForceNotApplicableMessage) {
		t.Fatalf("force on bindable pair status = %d body=%s", rec.Code, rec.Body.String())
	}
	longReason := strings.Repeat("x", assignment.MaxForcedReasonLen+1)
	rec = post(body(unpaidUnit, parentUnit, false, longReason), identity.RoleSupport)
	if rec.Code != http.StatusUnprocessableEntity ||
		strings.Contains(rec.Body.String(), assignment.ForcedReasonTooLongMessage) ||
		!strings.Contains(rec.Body.String(), assignmentapp.ChildOrderNotBindableMessage) {
		t.Fatalf("long reason with force false status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = post(body(linkedUnit, parentUnit, true, longReason), identity.RoleSupport)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), assignment.ForcedReasonTooLongMessage) {
		t.Fatalf("long reason status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = post(body(linkedUnit, parentUnit, false, ""), identity.RoleEditor)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = post(body(linkedUnit, parentUnit, false, ""), identity.RoleSupport)
	if rec.Code != http.StatusCreated {
		t.Fatalf("support create status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = post(body(linkedUnit, parentUnit, false, ""), identity.RoleAdmin)
	if rec.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAssignmentAdminHandler_RevokeOmitsFactsWhenLookupFails(t *testing.T) {
	parentItem, err := order.NewItem("card-var", "SKU-C", "Card", 1, shared.MustNewMoney(0, "EUR"))
	if err != nil {
		t.Fatalf("parent item: %v", err)
	}
	childItem, err := order.NewItem("ticket-var", "SKU-T", "Ticket", 1, shared.MustNewMoney(0, "EUR"))
	if err != nil {
		t.Fatalf("child item: %v", err)
	}
	parentOrder, err := order.NewOrder(id.New(), "other", "", "EUR", []order.Item{parentItem})
	if err != nil {
		t.Fatalf("parent order: %v", err)
	}
	childOrder, err := order.NewOrder(id.New(), "cust-1", "", "EUR", []order.Item{childItem})
	if err != nil {
		t.Fatalf("child order: %v", err)
	}
	parentUnit := assignment.UnitID(parentOrder.ID, "card-var")
	childUnit := assignment.UnitID(childOrder.ID, "ticket-var")
	repo := &adminAssignRepo{}
	svc := assignmentapp.NewService(repo)
	row, err := svc.CreateAssigned(context.Background(), parentUnit, childUnit)
	if err != nil {
		t.Fatalf("CreateAssigned: %v", err)
	}
	audits := &auditCapture{}
	var logs bytes.Buffer
	auditor := adminapp.NewAuditor(logger.NewWithWriter(&logs, "info"))
	auditor.SetAuditLogRepository(audits)
	flow := assignmentapp.NewFlow(svc, repo, adminAssignElig{ok: true}, adminAssignOrders{
		byID: map[string]*order.Order{
			parentOrder.ID: &parentOrder,
			childOrder.ID:  &childOrder,
		},
		batchErr: errors.New("facts down"),
	}, adminAssignVariants{byID: map[string]*catalog.Variant{
		"card-var":   {ID: "card-var", ProductID: "card-prod"},
		"ticket-var": {ID: "ticket-var", ProductID: "ticket-prod"},
	}}, adminAssignPayments{}).WithLinkedLeaves(
		adminAssignProducts{byID: map[string]*catalog.Product{
			"ticket-prod": {ID: "ticket-prod", Type: catalog.TypeSimple, Status: catalog.StatusActive, RequiresLinkedParent: true},
			"card-prod":   {ID: "card-prod", Type: catalog.TypeSimple, Status: catalog.StatusActive},
		}},
		adminAssignComponents{},
	)
	h := admin.NewAssignmentAdminHandler(flow, svc, auditor)
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/admin/assignments/{id}/revoke", admin.AdminContextMiddleware()(admin.RequirePermission(rbac.AssignmentsWrite)(h.Revoke())))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/assignments/"+row.ID+"/revoke", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), `"status":"revoked"`) ||
		!strings.Contains(rec.Body.String(), `"id":"`+row.ID+`"`) ||
		!strings.Contains(rec.Body.String(), `"child_customer_id":""`) ||
		!strings.Contains(rec.Body.String(), `"parent_customer_id":""`) ||
		strings.Contains(rec.Body.String(), `"child_customer_id":"cust-1"`) {
		t.Fatalf("revoke status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(audits.records) != 1 || audits.records[0].Action != "assignment.revoke" || audits.records[0].Result != "success" {
		t.Fatalf("audit = %+v", audits.records)
	}
	if strings.Count(logs.String(), "admin.assignment.facts_failed") != 1 ||
		!strings.Contains(logs.String(), `"route":"revoke"`) ||
		!strings.Contains(logs.String(), `"assignment_id":"`+row.ID+`"`) ||
		!strings.Contains(logs.String(), `"error":"assignment flow: lookup orders: facts down"`) ||
		strings.Contains(logs.String(), `"count":`) {
		t.Fatalf("log = %s", logs.String())
	}
	got, err := svc.Get(context.Background(), row.ID)
	if err != nil || got == nil || got.Status() != assignment.StatusRevoked {
		t.Fatalf("stored after revoke = %+v %v", got, err)
	}
}

func TestAssignmentAdminHandler_ListAndCurrentOmitFactsWhenLookupFails(t *testing.T) {
	parentItem, err := order.NewItem("card-var", "SKU-C", "Card", 1, shared.MustNewMoney(0, "EUR"))
	if err != nil {
		t.Fatalf("parent item: %v", err)
	}
	childItem, err := order.NewItem("ticket-var", "SKU-T", "Ticket", 1, shared.MustNewMoney(0, "EUR"))
	if err != nil {
		t.Fatalf("child item: %v", err)
	}
	parentOrder, err := order.NewOrder(id.New(), "other", "", "EUR", []order.Item{parentItem})
	if err != nil {
		t.Fatalf("parent order: %v", err)
	}
	childOrder, err := order.NewOrder(id.New(), "cust-1", "", "EUR", []order.Item{childItem})
	if err != nil {
		t.Fatalf("child order: %v", err)
	}
	parentUnit := assignment.UnitID(parentOrder.ID, "card-var")
	childUnit := assignment.UnitID(childOrder.ID, "ticket-var")
	repo := &adminAssignRepo{}
	svc := assignmentapp.NewService(repo)
	row, err := svc.CreateAssigned(context.Background(), parentUnit, childUnit)
	if err != nil {
		t.Fatalf("CreateAssigned: %v", err)
	}
	row2, err := svc.CreateAssigned(context.Background(), parentUnit, assignment.UnitID(id.New(), "ticket-var"))
	if err != nil {
		t.Fatalf("CreateAssigned second: %v", err)
	}
	var logs bytes.Buffer
	auditor := adminapp.NewAuditor(logger.NewWithWriter(&logs, "info"))
	flow := assignmentapp.NewFlow(svc, repo, adminAssignElig{ok: true}, adminAssignOrders{
		byID: map[string]*order.Order{
			parentOrder.ID: &parentOrder,
			childOrder.ID:  &childOrder,
		},
		batchErr: errors.New("facts down"),
	}, adminAssignVariants{byID: map[string]*catalog.Variant{
		"card-var":   {ID: "card-var", ProductID: "card-prod"},
		"ticket-var": {ID: "ticket-var", ProductID: "ticket-prod"},
	}}, adminAssignPayments{}).WithLinkedLeaves(
		adminAssignProducts{byID: map[string]*catalog.Product{
			"ticket-prod": {ID: "ticket-prod", Type: catalog.TypeSimple, Status: catalog.StatusActive, RequiresLinkedParent: true},
			"card-prod":   {ID: "card-prod", Type: catalog.TypeSimple, Status: catalog.StatusActive},
		}},
		adminAssignComponents{},
	)
	h := admin.NewAssignmentAdminHandler(flow, svc, auditor)
	mux := http.NewServeMux()
	withAdmin := admin.AdminContextMiddleware()
	requireRead := admin.RequirePermission(rbac.AssignmentsRead)
	mux.Handle("GET /api/v1/admin/assignments", withAdmin(requireRead(h.ListByParent())))
	mux.Handle("GET /api/v1/admin/assignments/current", withAdmin(requireRead(h.GetCurrent())))

	assertEmptyFacts := func(rec *httptest.ResponseRecorder) {
		t.Helper()
		body := rec.Body.String()
		if rec.Code != http.StatusOK ||
			!strings.Contains(body, `"id":"`+row.ID+`"`) ||
			!strings.Contains(body, `"status":"assigned"`) ||
			!strings.Contains(body, `"child_customer_id":""`) ||
			!strings.Contains(body, `"child_order_status":""`) ||
			!strings.Contains(body, `"parent_customer_id":""`) ||
			!strings.Contains(body, `"parent_order_status":""`) ||
			strings.Contains(body, `"child_customer_id":"cust-1"`) {
			t.Fatalf("status=%d body=%s", rec.Code, body)
		}
	}

	q := url.Values{}
	q.Set("parent_unit_id", parentUnit)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/assignments?"+q.Encode(), nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)
	assertEmptyFacts(rec)
	if !strings.Contains(rec.Body.String(), `"id":"`+row2.ID+`"`) ||
		strings.Count(logs.String(), "admin.assignment.facts_failed") != 1 ||
		!strings.Contains(logs.String(), `"route":"list"`) ||
		!strings.Contains(logs.String(), `"error":"assignment flow: lookup orders: facts down"`) ||
		!strings.Contains(logs.String(), `"count":2`) ||
		strings.Contains(logs.String(), `"assignment_id"`) {
		t.Fatalf("list log = %s body=%s", logs.String(), rec.Body.String())
	}

	q = url.Values{}
	q.Set("child_unit_id", childUnit)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/assignments/current?"+q.Encode(), nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)
	assertEmptyFacts(rec)
	if strings.Count(logs.String(), "admin.assignment.facts_failed") != 2 ||
		!strings.Contains(logs.String(), `"route":"current"`) ||
		!strings.Contains(logs.String(), `"assignment_id":"`+row.ID+`"`) ||
		!strings.Contains(logs.String(), `"error":"assignment flow: lookup orders: facts down"`) {
		t.Fatalf("current log = %s", logs.String())
	}
}

func TestAssignmentAdminHandler_ListDefaultLimitCanonical(t *testing.T) {
	parent := assignment.UnitID(id.New(), "card-var")
	repo := &adminAssignRepo{}
	mux := newAssignmentAdminMux(t, repo, adminAssignOrders{}, adminAssignVariants{}, &auditCapture{})
	svc := assignmentapp.NewService(repo)
	ctx := context.Background()
	if _, err := svc.CreateAssigned(ctx, parent, assignment.UnitID(id.New(), "ticket-a")); err != nil {
		t.Fatalf("assign a: %v", err)
	}
	if _, err := svc.CreateAssigned(ctx, parent, assignment.UnitID(id.New(), "ticket-b")); err != nil {
		t.Fatalf("assign b: %v", err)
	}
	revoked, err := svc.CreateAssigned(ctx, parent, assignment.UnitID(id.New(), "ticket-c"))
	if err != nil {
		t.Fatalf("assign c: %v", err)
	}
	if _, _, err := svc.Revoke(ctx, revoked.ID); err != nil {
		t.Fatalf("revoke c: %v", err)
	}

	q := url.Values{}
	q.Set("parent_unit_id", " "+parent+" ")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/assignments?"+q.Encode(), nil)
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.Count(rec.Body.String(), `"status":"assigned"`) != 2 || strings.Contains(rec.Body.String(), `"status":"revoked"`) {
		t.Fatalf("default list status=%d body=%s", rec.Code, rec.Body.String())
	}

	q.Set("limit", "1")
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/assignments?"+q.Encode(), nil)
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.Count(rec.Body.String(), `"status":"assigned"`) != 1 || !strings.Contains(rec.Body.String(), `"has_more":true`) {
		t.Fatalf("limit list status=%d body=%s", rec.Code, rec.Body.String())
	}
}
