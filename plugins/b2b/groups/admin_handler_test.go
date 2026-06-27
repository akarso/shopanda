package groups_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/customergroup"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/plugins/b2b/groups"
)

type stubGroupRepo struct {
	byID   map[string]customergroup.Group
	byCode map[string]customergroup.Group
	byCust map[string]string
}

func newStubGroupRepo() *stubGroupRepo {
	return &stubGroupRepo{
		byID:   make(map[string]customergroup.Group),
		byCode: make(map[string]customergroup.Group),
		byCust: make(map[string]string),
	}
}

func (s *stubGroupRepo) List(_ context.Context, offset, limit int) ([]customergroup.Group, error) {
	var out []customergroup.Group
	for _, g := range s.byID {
		out = append(out, g)
	}
	if offset >= len(out) {
		return nil, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], nil
}

func (s *stubGroupRepo) FindByID(_ context.Context, groupID string) (*customergroup.Group, error) {
	g, ok := s.byID[groupID]
	if !ok {
		return nil, nil
	}
	return &g, nil
}

func (s *stubGroupRepo) FindByCode(_ context.Context, code string) (*customergroup.Group, error) {
	g, ok := s.byCode[code]
	if !ok {
		return nil, nil
	}
	return &g, nil
}

func (s *stubGroupRepo) Save(_ context.Context, group *customergroup.Group) error {
	s.byID[group.ID] = *group
	s.byCode[group.Code] = *group
	return nil
}

func (s *stubGroupRepo) AssignCustomer(_ context.Context, customerID, groupID string) error {
	s.byCust[customerID] = groupID
	return nil
}

func (s *stubGroupRepo) RemoveCustomer(_ context.Context, customerID string) error {
	delete(s.byCust, customerID)
	return nil
}

func (s *stubGroupRepo) FindGroupByCustomerID(_ context.Context, customerID string) (*customergroup.Group, error) {
	groupID, ok := s.byCust[customerID]
	if !ok {
		return nil, nil
	}
	g, ok := s.byID[groupID]
	if !ok {
		return nil, nil
	}
	return &g, nil
}

type stubCustomerRepo struct {
	byID map[string]*customer.Customer
}

func newStubCustomerRepo() *stubCustomerRepo {
	return &stubCustomerRepo{byID: make(map[string]*customer.Customer)}
}

func (s *stubCustomerRepo) FindByID(_ context.Context, customerID string) (*customer.Customer, error) {
	return s.byID[customerID], nil
}

func (s *stubCustomerRepo) FindByEmail(_ context.Context, _ string) (*customer.Customer, error) {
	return nil, nil
}

func (s *stubCustomerRepo) Create(_ context.Context, _ *customer.Customer) error { return nil }

func (s *stubCustomerRepo) Update(_ context.Context, _ *customer.Customer) error { return nil }

func (s *stubCustomerRepo) ListCustomers(_ context.Context, _, _ int) ([]customer.Customer, error) {
	return nil, nil
}

func (s *stubCustomerRepo) BumpTokenGeneration(_ context.Context, _ string) error { return nil }

func (s *stubCustomerRepo) ChangePasswordAndBumpTokenGeneration(_ context.Context, _, _ string) error {
	return nil
}

func (s *stubCustomerRepo) Delete(_ context.Context, _ string) error { return nil }

func newGroupAdminRouter(h *groups.AdminHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/customer-groups", h.List())
	mux.HandleFunc("GET /api/v1/admin/customer-groups/{groupId}", h.Get())
	mux.HandleFunc("POST /api/v1/admin/customer-groups", h.Create())
	mux.HandleFunc("PUT /api/v1/admin/customer-groups/{groupId}", h.Update())
	mux.HandleFunc("GET /api/v1/admin/customers/{customerId}/customer-group", h.GetCustomerGroup())
	mux.HandleFunc("PUT /api/v1/admin/customers/{customerId}/customer-group", h.AssignCustomer())
	mux.HandleFunc("DELETE /api/v1/admin/customers/{customerId}/customer-group", h.RemoveCustomer())
	return mux
}

func TestAdminHandler_CreateAndGet(t *testing.T) {
	repo := newStubGroupRepo()
	h := groups.NewAdminHandler(repo, newStubCustomerRepo())
	router := newGroupAdminRouter(h)

	body := bytes.NewBufferString(`{"code":"wholesale","name":"Wholesale","description":"Tier 1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/customer-groups", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var created struct {
		Data struct {
			Group struct {
				ID   string `json:"id"`
				Code string `json:"code"`
			} `json:"group"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Data.Group.Code != "wholesale" {
		t.Fatalf("code = %q", created.Data.Group.Code)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/customer-groups/"+created.Data.Group.ID, nil)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", getRec.Code, getRec.Body.String())
	}
}

func TestAdminHandler_CreateDuplicateCodeRejected(t *testing.T) {
	repo := newStubGroupRepo()
	g, err := customergroup.NewGroup(id.New(), "vip", "VIP", "")
	if err != nil {
		t.Fatalf("NewGroup: %v", err)
	}
	if err := repo.Save(context.Background(), &g); err != nil {
		t.Fatalf("Save: %v", err)
	}

	h := groups.NewAdminHandler(repo, newStubCustomerRepo())
	router := newGroupAdminRouter(h)
	body := bytes.NewBufferString(`{"code":"vip","name":"Another VIP","description":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/customer-groups", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestAdminHandler_AssignCustomerGroup(t *testing.T) {
	groupRepo := newStubGroupRepo()
	customerRepo := newStubCustomerRepo()
	g, err := customergroup.NewGroup(id.New(), "b2b", "B2B", "")
	if err != nil {
		t.Fatalf("NewGroup: %v", err)
	}
	if err := groupRepo.Save(context.Background(), &g); err != nil {
		t.Fatalf("Save group: %v", err)
	}

	customerID := id.New()
	customerRepo.byID[customerID] = &customer.Customer{ID: customerID, Email: "buyer@example.com"}

	h := groups.NewAdminHandler(groupRepo, customerRepo)
	router := newGroupAdminRouter(h)
	body := bytes.NewBufferString(`{"group_id":"` + g.ID + `"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/customers/"+customerID+"/customer-group", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("assign status = %d, body = %s", rec.Code, rec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/customers/"+customerID+"/customer-group", nil)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get membership status = %d", getRec.Code)
	}
}

func TestAdminHandler_GetCustomerGroup_UnassignedReturnsNull(t *testing.T) {
	h := groups.NewAdminHandler(newStubGroupRepo(), newStubCustomerRepo())
	router := newGroupAdminRouter(h)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/customers/"+id.New()+"/customer-group", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"group":null`)) {
		t.Fatalf("body = %s, want group:null", rec.Body.String())
	}
}
