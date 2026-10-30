package admin_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	catalogApp "github.com/akarso/shopanda/internal/application/catalog"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/interfaces/http/admin"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type compBundleRepo struct {
	catalog.BundleComponentRepository
	list    []catalog.BundleComponent
	listed  bool
	upsert  *catalog.BundleComponent
	removed struct {
		bundleID    string
		componentID string
	}
	removeErr error
	parent    *compBundleRepo
}

func (s *compBundleRepo) owner() *compBundleRepo {
	if s.parent != nil {
		return s.parent
	}
	return s
}

func (s *compBundleRepo) ListByBundleID(context.Context, string) ([]catalog.BundleComponent, error) {
	s.owner().listed = true
	if s.list == nil {
		return []catalog.BundleComponent{}, nil
	}
	out := make([]catalog.BundleComponent, len(s.list))
	copy(out, s.list)
	return out, nil
}

func (s *compBundleRepo) CountByBundleID(context.Context, string) (int, error) { return 0, nil }
func (s *compBundleRepo) CountByComponentID(context.Context, string) (int, error) {
	return 0, nil
}
func (s *compBundleRepo) Upsert(_ context.Context, c *catalog.BundleComponent) error {
	cp := *c
	s.upsert = &cp
	for i := range s.list {
		if s.list[i].BundleProductID == cp.BundleProductID && s.list[i].ComponentProductID == cp.ComponentProductID {
			s.list[i] = cp
			return nil
		}
	}
	s.list = append(s.list, cp)
	return nil
}
func (s *compBundleRepo) Remove(_ context.Context, bundleID, componentID string) error {
	s.removed.bundleID = bundleID
	s.removed.componentID = componentID
	return s.removeErr
}
func (s *compBundleRepo) WithTx(*sql.Tx) catalog.BundleComponentRepository {
	if s.parent != nil {
		return s
	}
	child := &compBundleRepo{
		list:   append([]catalog.BundleComponent(nil), s.list...),
		parent: s,
	}
	if tx := adminFakeTxCurrent.Load(); tx != nil {
		tx.onCommit = append(tx.onCommit, func() {
			s.list = append([]catalog.BundleComponent(nil), child.list...)
			if child.upsert != nil {
				cp := *child.upsert
				s.upsert = &cp
			}
		})
	}
	return child
}

type compGroupedRepo struct {
	catalog.GroupedMemberRepository
	list    []catalog.GroupedMember
	insert  *catalog.GroupedMember
	removed struct {
		groupID  string
		memberID string
	}
	removeErr error
}

func (s *compGroupedRepo) ListByGroupID(context.Context, string) ([]catalog.GroupedMember, error) {
	if s.list == nil {
		return []catalog.GroupedMember{}, nil
	}
	return s.list, nil
}
func (s *compGroupedRepo) CountByGroupID(context.Context, string) (int, error)  { return 0, nil }
func (s *compGroupedRepo) CountByMemberID(context.Context, string) (int, error) { return 0, nil }
func (s *compGroupedRepo) Insert(_ context.Context, m *catalog.GroupedMember) error {
	cp := *m
	s.insert = &cp
	return nil
}
func (s *compGroupedRepo) Remove(_ context.Context, groupID, memberID string) error {
	s.removed.groupID = groupID
	s.removed.memberID = memberID
	return s.removeErr
}
func (s *compGroupedRepo) WithTx(*sql.Tx) catalog.GroupedMemberRepository { return s }

func newCompositionHandler(products *mockAdminProductRepo, components *compBundleRepo, members *compGroupedRepo) *admin.ProductCompositionAdminHandler {
	if components == nil {
		components = &compBundleRepo{}
	}
	if members == nil {
		members = &compGroupedRepo{}
	}
	bundles := catalogApp.NewBundleService(products, components, adminFakeTxDBConn())
	grouped := catalogApp.NewGroupedService(products, members, adminFakeTxDBConn())
	return admin.NewProductCompositionAdminHandler(products, bundles, grouped, adminapp.NewAuditor(logger.New("error")))
}

func compositionProducts(rows ...*catalog.Product) *mockAdminProductRepo {
	byID := map[string]*catalog.Product{}
	for _, p := range rows {
		byID[p.ID] = p
	}
	return &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return byID[id], nil
		},
	}
}

func compositionRequest(method, target string, body any, path map[string]string) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, target, &buf)
	for k, v := range path {
		req.SetPathValue(k, v)
	}
	return req
}

func decodeData(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope struct {
		Data  map[string]any `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, rec.Body.String())
	}
	if envelope.Error != nil {
		t.Fatalf("unexpected error: %+v body=%s", envelope.Error, rec.Body.String())
	}
	return envelope.Data
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) (string, string) {
	t.Helper()
	var envelope struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, rec.Body.String())
	}
	if envelope.Error == nil {
		t.Fatalf("expected error, body=%s", rec.Body.String())
	}
	return envelope.Error.Code, envelope.Error.Message
}

func TestProductCompositionAdmin_ListBundleComponents_EnrichesChild(t *testing.T) {
	created := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	products := compositionProducts(
		&catalog.Product{ID: "b1", Name: "Kit", Slug: "kit", Type: catalog.TypeBundle, Status: catalog.StatusActive},
		&catalog.Product{ID: "c1", Name: "Leaf", Slug: "leaf", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	)
	components := &compBundleRepo{list: []catalog.BundleComponent{{
		BundleProductID: "b1", ComponentProductID: "c1", Quantity: 2, Required: true, CreatedAt: created,
	}}}
	h := newCompositionHandler(products, components, nil)

	rec := httptest.NewRecorder()
	h.ListBundleComponents().ServeHTTP(rec, compositionRequest(http.MethodGet, "/api/v1/admin/products/b1/bundle-components", nil, map[string]string{"id": "b1"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	data := decodeData(t, rec)
	rows, _ := data["components"].([]any)
	if len(rows) != 1 {
		t.Fatalf("components = %#v", data["components"])
	}
	row := rows[0].(map[string]any)
	if row["component_product_id"] != "c1" || row["name"] != "Leaf" || row["slug"] != "leaf" || row["type"] != "simple" {
		t.Fatalf("row = %#v", row)
	}
	if row["quantity"] != float64(2) || row["required"] != true {
		t.Fatalf("qty/required = %#v %#v", row["quantity"], row["required"])
	}
}

func TestProductCompositionAdmin_ListBundleComponents_WrongParentType(t *testing.T) {
	products := compositionProducts(
		&catalog.Product{ID: "s1", Name: "Simple", Slug: "simple", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	)
	h := newCompositionHandler(products, nil, nil)
	rec := httptest.NewRecorder()
	h.ListBundleComponents().ServeHTTP(rec, compositionRequest(http.MethodGet, "/api/v1/admin/products/s1/bundle-components", nil, map[string]string{"id": "s1"}))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 body=%s", rec.Code, rec.Body.String())
	}
	code, msg := decodeError(t, rec)
	if code != "validation" || msg == "" {
		t.Fatalf("error = %s %s", code, msg)
	}
}

func TestProductCompositionAdmin_AddBundleComponent_OmittedRequiredDefaultsTrue(t *testing.T) {
	products := compositionProducts(
		&catalog.Product{ID: "b1", Type: catalog.TypeBundle, Status: catalog.StatusActive, Name: "Kit", Slug: "kit"},
		&catalog.Product{ID: "c1", Type: catalog.TypeVirtual, Status: catalog.StatusActive, Name: "Virt", Slug: "virt"},
	)
	components := &compBundleRepo{}
	h := newCompositionHandler(products, components, nil)
	rec := httptest.NewRecorder()
	h.AddBundleComponent().ServeHTTP(rec, compositionRequest(http.MethodPost, "/api/v1/admin/products/b1/bundle-components", map[string]any{
		"component_product_id": "c1",
		"quantity":             3,
	}, map[string]string{"id": "b1"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if components.upsert == nil || !components.upsert.Required || components.upsert.Quantity != 3 {
		t.Fatalf("upsert = %+v", components.upsert)
	}
	data := decodeData(t, rec)
	component := data["component"].(map[string]any)
	if component["required"] != true || component["name"] != "Virt" || component["type"] != "virtual" {
		t.Fatalf("component = %#v", component)
	}
}

func TestProductCompositionAdmin_AddBundleComponent_ExplicitRequiredFalse(t *testing.T) {
	products := compositionProducts(
		&catalog.Product{ID: "b1", Type: catalog.TypeBundle, Status: catalog.StatusActive},
		&catalog.Product{ID: "c1", Type: catalog.TypeDownloadable, Status: catalog.StatusActive, Name: "File", Slug: "file"},
	)
	components := &compBundleRepo{}
	h := newCompositionHandler(products, components, nil)
	rec := httptest.NewRecorder()
	h.AddBundleComponent().ServeHTTP(rec, compositionRequest(http.MethodPost, "/x", map[string]any{
		"component_product_id": "c1",
		"quantity":             1,
		"required":             false,
	}, map[string]string{"id": "b1"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if components.upsert == nil || components.upsert.Required {
		t.Fatalf("upsert = %+v, want required false", components.upsert)
	}
}

func TestProductCompositionAdmin_AddBundleComponent_InvalidBody(t *testing.T) {
	products := compositionProducts(&catalog.Product{ID: "b1", Type: catalog.TypeBundle, Status: catalog.StatusActive})
	h := newCompositionHandler(products, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/x", bytes.NewBufferString("{"))
	req.SetPathValue("id", "b1")
	rec := httptest.NewRecorder()
	h.AddBundleComponent().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestProductCompositionAdmin_RemoveBundleComponent_MissingIs404(t *testing.T) {
	products := compositionProducts(&catalog.Product{ID: "b1", Type: catalog.TypeBundle, Status: catalog.StatusActive})
	components := &compBundleRepo{removeErr: apperror.NotFound("bundle component not found")}
	h := newCompositionHandler(products, components, nil)
	rec := httptest.NewRecorder()
	h.RemoveBundleComponent().ServeHTTP(rec, compositionRequest(http.MethodDelete, "/x", nil, map[string]string{
		"id": "b1", "componentId": "missing",
	}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	code, _ := decodeError(t, rec)
	if code != "not_found" {
		t.Fatalf("code = %s", code)
	}
	if components.removed.componentID != "missing" {
		t.Fatalf("removed = %+v", components.removed)
	}
}

func TestProductCompositionAdmin_RemoveBundleComponent_WrongParentType(t *testing.T) {
	products := compositionProducts(&catalog.Product{ID: "s1", Type: catalog.TypeSimple, Status: catalog.StatusActive})
	components := &compBundleRepo{}
	h := newCompositionHandler(products, components, nil)
	rec := httptest.NewRecorder()
	h.RemoveBundleComponent().ServeHTTP(rec, compositionRequest(http.MethodDelete, "/x", nil, map[string]string{
		"id": "s1", "componentId": "c1",
	}))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if components.removed.componentID != "" {
		t.Fatalf("remove called for wrong parent type: %+v", components.removed)
	}
	if components.listed {
		t.Fatal("delete must not list the composition to reject the parent type")
	}
}

func TestProductCompositionAdmin_GroupedMembers(t *testing.T) {
	products := compositionProducts(
		&catalog.Product{ID: "g1", Type: catalog.TypeGrouped, Status: catalog.StatusActive, Name: "Group", Slug: "group"},
		&catalog.Product{ID: "m1", Type: catalog.TypeSimple, Status: catalog.StatusActive, Name: "Member", Slug: "member"},
	)
	members := &compGroupedRepo{list: []catalog.GroupedMember{{
		GroupProductID: "g1", MemberProductID: "m1",
	}}}
	h := newCompositionHandler(products, nil, members)

	listRec := httptest.NewRecorder()
	h.ListGroupedMembers().ServeHTTP(listRec, compositionRequest(http.MethodGet, "/x", nil, map[string]string{"id": "g1"}))
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	data := decodeData(t, listRec)
	rows := data["members"].([]any)
	row := rows[0].(map[string]any)
	if row["member_product_id"] != "m1" || row["slug"] != "member" || row["type"] != "simple" {
		t.Fatalf("member = %#v", row)
	}

	addRec := httptest.NewRecorder()
	h.AddGroupedMember().ServeHTTP(addRec, compositionRequest(http.MethodPost, "/x", map[string]any{
		"member_product_id": "m1",
	}, map[string]string{"id": "g1"}))
	if addRec.Code != http.StatusOK {
		t.Fatalf("add status = %d body=%s", addRec.Code, addRec.Body.String())
	}
	if members.insert == nil || members.insert.MemberProductID != "m1" || members.insert.GroupProductID != "g1" {
		t.Fatalf("insert = %+v", members.insert)
	}

	members.removeErr = apperror.NotFound("grouped member not found")
	delRec := httptest.NewRecorder()
	h.RemoveGroupedMember().ServeHTTP(delRec, compositionRequest(http.MethodDelete, "/x", nil, map[string]string{
		"id": "g1", "memberId": "gone",
	}))
	if delRec.Code != http.StatusNotFound {
		t.Fatalf("delete status = %d body=%s", delRec.Code, delRec.Body.String())
	}
}

func TestProductCompositionAdmin_ListGroupedMembers_WrongParentType(t *testing.T) {
	products := compositionProducts(&catalog.Product{ID: "b1", Type: catalog.TypeBundle, Status: catalog.StatusActive})
	h := newCompositionHandler(products, nil, nil)
	rec := httptest.NewRecorder()
	h.ListGroupedMembers().ServeHTTP(rec, compositionRequest(http.MethodGet, "/x", nil, map[string]string{"id": "b1"}))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestNewProductCompositionAdminHandler_NilServicePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for nil bundle service")
		}
	}()
	admin.NewProductCompositionAdminHandler(compositionProducts(), nil, &catalogApp.GroupedService{}, adminapp.NewAuditor(logger.New("error")))
}

func TestProductCompositionAdmin_AddBundleComponent_Validation(t *testing.T) {
	products := compositionProducts(
		&catalog.Product{ID: "b1", Type: catalog.TypeBundle, Status: catalog.StatusActive, Name: "Kit", Slug: "kit"},
		&catalog.Product{ID: "b2", Type: catalog.TypeBundle, Status: catalog.StatusActive, Name: "Other", Slug: "other"},
	)
	h := newCompositionHandler(products, nil, nil)
	cases := []struct {
		name string
		body map[string]any
		want string
	}{
		{name: "missing component id", body: map[string]any{"quantity": 1}, want: "component product id"},
		{name: "qty zero", body: map[string]any{"component_product_id": "c1", "quantity": 0}, want: "at least 1"},
		{name: "qty 10000", body: map[string]any{"component_product_id": "c1", "quantity": 10000}, want: "9999"},
		{name: "self-link", body: map[string]any{"component_product_id": "b1", "quantity": 1}, want: "itself"},
		{name: "non-leaf", body: map[string]any{"component_product_id": "b2", "quantity": 1}, want: "simple, virtual, or downloadable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.AddBundleComponent().ServeHTTP(rec, compositionRequest(http.MethodPost, "/x", tc.body, map[string]string{"id": "b1"}))
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			_, msg := decodeError(t, rec)
			if msg == "" || !bytes.Contains([]byte(msg), []byte(tc.want)) {
				t.Fatalf("message = %q, want %q", msg, tc.want)
			}
		})
	}
}

func TestProductCompositionAdmin_AddBundleComponent_MissingChild(t *testing.T) {
	products := compositionProducts(
		&catalog.Product{ID: "b1", Type: catalog.TypeBundle, Status: catalog.StatusActive},
	)
	h := newCompositionHandler(products, nil, nil)
	rec := httptest.NewRecorder()
	h.AddBundleComponent().ServeHTTP(rec, compositionRequest(http.MethodPost, "/x", map[string]any{
		"component_product_id": "missing",
		"quantity":             1,
	}, map[string]string{"id": "b1"}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestProductCompositionAdmin_AddBundleComponent_EmptyBody(t *testing.T) {
	h := newCompositionHandler(compositionProducts(&catalog.Product{ID: "b1", Type: catalog.TypeBundle, Status: catalog.StatusActive}), nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(nil))
	req.SetPathValue("id", "b1")
	rec := httptest.NewRecorder()
	h.AddBundleComponent().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	_, msg := decodeError(t, rec)
	if msg != "invalid request body" {
		t.Fatalf("message = %q", msg)
	}
}

func TestProductCompositionAdmin_AddBundleComponent_EnrichmentFailureStillOK(t *testing.T) {
	byID := map[string]*catalog.Product{
		"b1": {ID: "b1", Type: catalog.TypeBundle, Status: catalog.StatusActive, Name: "Kit", Slug: "kit"},
		"c1": {ID: "c1", Type: catalog.TypeSimple, Status: catalog.StatusActive, Name: "Leaf", Slug: "leaf"},
	}
	childReads := 0
	products := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			if id == "c1" {
				childReads++
				if childReads >= 3 {
					return nil, errors.New("child lookup failed")
				}
			}
			return byID[id], nil
		},
	}
	components := &compBundleRepo{}
	h := newCompositionHandler(products, components, nil)
	rec := httptest.NewRecorder()
	h.AddBundleComponent().ServeHTTP(rec, compositionRequest(http.MethodPost, "/x", map[string]any{
		"component_product_id": "c1",
		"quantity":             4,
		"required":             true,
	}, map[string]string{"id": "b1"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if components.upsert == nil {
		t.Fatal("expected committed upsert")
	}
	data := decodeData(t, rec)
	component := data["component"].(map[string]any)
	if component["component_product_id"] != "c1" || component["quantity"] != float64(4) || component["required"] != true {
		t.Fatalf("component = %#v", component)
	}
	if component["name"] != "" || component["slug"] != "" || component["type"] != "" {
		t.Fatalf("enrichment failure leaked into dto: %#v", component)
	}
}

func TestProductCompositionAdmin_ListBundleComponents_ChildErrorsDoNotFailList(t *testing.T) {
	products := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			switch id {
			case "b1":
				return &catalog.Product{ID: "b1", Type: catalog.TypeBundle, Status: catalog.StatusActive}, nil
			case "ok":
				return &catalog.Product{ID: "ok", Name: "Leaf", Slug: "leaf", Type: catalog.TypeSimple}, nil
			case "bad":
				return nil, errors.New("child lookup failed")
			default:
				return nil, nil
			}
		},
	}
	components := &compBundleRepo{list: []catalog.BundleComponent{
		{BundleProductID: "b1", ComponentProductID: "ok", Quantity: 1, Required: true},
		{BundleProductID: "b1", ComponentProductID: "bad", Quantity: 2, Required: false},
		{BundleProductID: "b1", ComponentProductID: "gone", Quantity: 3, Required: true},
	}}
	h := newCompositionHandler(products, components, nil)
	rec := httptest.NewRecorder()
	h.ListBundleComponents().ServeHTTP(rec, compositionRequest(http.MethodGet, "/x", nil, map[string]string{"id": "b1"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	data := decodeData(t, rec)
	rows := data["components"].([]any)
	if len(rows) != 3 {
		t.Fatalf("components = %#v", rows)
	}
	ok := rows[0].(map[string]any)
	if ok["name"] != "Leaf" || ok["slug"] != "leaf" || ok["type"] != "simple" {
		t.Fatalf("ok row = %#v", ok)
	}
	for _, raw := range rows[1:] {
		row := raw.(map[string]any)
		if row["name"] != "" || row["slug"] != "" || row["type"] != "" {
			t.Fatalf("broken child row = %#v", row)
		}
	}
}

type batchProductRepo struct {
	*mockAdminProductRepo
	listByIDsFn    func(context.Context, []string) (map[string]*catalog.Product, error)
	listByIDsCalls int
}

func (r *batchProductRepo) ListByIDs(ctx context.Context, ids []string) (map[string]*catalog.Product, error) {
	r.listByIDsCalls++
	return r.listByIDsFn(ctx, ids)
}

func TestProductCompositionAdmin_ListBundleComponents_BatchesChildren(t *testing.T) {
	base := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			if id == "c1" {
				t.Fatal("list must not FindByID each child")
			}
			if id == "b1" {
				return &catalog.Product{ID: "b1", Type: catalog.TypeBundle, Status: catalog.StatusActive}, nil
			}
			return nil, nil
		},
	}
	products := &batchProductRepo{
		mockAdminProductRepo: base,
		listByIDsFn: func(_ context.Context, ids []string) (map[string]*catalog.Product, error) {
			if len(ids) != 2 || ids[0] != "c1" || ids[1] != "missing" {
				t.Fatalf("ids = %v", ids)
			}
			return map[string]*catalog.Product{
				"c1": {ID: "c1", Name: "Leaf", Slug: "leaf", Type: catalog.TypeSimple},
			}, nil
		},
	}
	components := &compBundleRepo{list: []catalog.BundleComponent{
		{BundleProductID: "b1", ComponentProductID: "c1", Quantity: 1, Required: true},
		{BundleProductID: "b1", ComponentProductID: "missing", Quantity: 1, Required: false},
	}}
	h := admin.NewProductCompositionAdminHandler(products, catalogApp.NewBundleService(products, components, adminFakeTxDBConn()), catalogApp.NewGroupedService(products, &compGroupedRepo{}, adminFakeTxDBConn()), adminapp.NewAuditor(logger.New("error")))
	rec := httptest.NewRecorder()
	h.ListBundleComponents().ServeHTTP(rec, compositionRequest(http.MethodGet, "/x", nil, map[string]string{"id": "b1"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if products.listByIDsCalls != 1 {
		t.Fatalf("ListByIDs calls = %d, want 1", products.listByIDsCalls)
	}
	data := decodeData(t, rec)
	rows := data["components"].([]any)
	first := rows[0].(map[string]any)
	second := rows[1].(map[string]any)
	if first["name"] != "Leaf" || second["name"] != "" || second["slug"] != "" || second["type"] != "" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestProductCompositionAdmin_List_ListByIDsErrorFailsRequest(t *testing.T) {
	base := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			switch id {
			case "b1":
				return &catalog.Product{ID: "b1", Type: catalog.TypeBundle, Status: catalog.StatusActive}, nil
			case "g1":
				return &catalog.Product{ID: "g1", Type: catalog.TypeGrouped, Status: catalog.StatusActive}, nil
			default:
				t.Fatalf("FindByID %s must not blank children after a batch error", id)
				return nil, nil
			}
		},
	}
	products := &batchProductRepo{
		mockAdminProductRepo: base,
		listByIDsFn: func(context.Context, []string) (map[string]*catalog.Product, error) {
			return nil, errors.New("db down")
		},
	}
	components := &compBundleRepo{list: []catalog.BundleComponent{
		{BundleProductID: "b1", ComponentProductID: "c1", Quantity: 1, Required: true},
	}}
	members := &compGroupedRepo{list: []catalog.GroupedMember{
		{GroupProductID: "g1", MemberProductID: "m1"},
	}}
	h := admin.NewProductCompositionAdminHandler(products, catalogApp.NewBundleService(products, components, adminFakeTxDBConn()), catalogApp.NewGroupedService(products, members, adminFakeTxDBConn()), adminapp.NewAuditor(logger.New("error")))

	bundleRec := httptest.NewRecorder()
	h.ListBundleComponents().ServeHTTP(bundleRec, compositionRequest(http.MethodGet, "/x", nil, map[string]string{"id": "b1"}))
	if bundleRec.Code != http.StatusInternalServerError {
		t.Fatalf("bundle status = %d body=%s", bundleRec.Code, bundleRec.Body.String())
	}
	if bytes.Contains(bundleRec.Body.Bytes(), []byte(`"components"`)) {
		t.Fatalf("batch error returned a component list: %s", bundleRec.Body.String())
	}

	groupedRec := httptest.NewRecorder()
	h.ListGroupedMembers().ServeHTTP(groupedRec, compositionRequest(http.MethodGet, "/x", nil, map[string]string{"id": "g1"}))
	if groupedRec.Code != http.StatusInternalServerError {
		t.Fatalf("grouped status = %d body=%s", groupedRec.Code, groupedRec.Body.String())
	}
}

func TestProductCompositionAdmin_AddBundleComponent_RequiredCap(t *testing.T) {
	productsList := []*catalog.Product{{
		ID: "b1", Type: catalog.TypeBundle, Status: catalog.StatusActive, Name: "Kit", Slug: "kit",
	}}
	rows := make([]catalog.BundleComponent, catalog.MaxBundleRequiredComponents)
	for i := range rows {
		id := "c" + strconv.Itoa(i)
		productsList = append(productsList, &catalog.Product{
			ID: id, Type: catalog.TypeSimple, Status: catalog.StatusActive, Name: id, Slug: id,
		})
		rows[i] = catalog.BundleComponent{BundleProductID: "b1", ComponentProductID: id, Quantity: 1, Required: true}
	}
	productsList = append(productsList, &catalog.Product{
		ID: "extra", Type: catalog.TypeSimple, Status: catalog.StatusActive, Name: "Extra", Slug: "extra",
	})
	components := &compBundleRepo{list: rows}
	h := newCompositionHandler(compositionProducts(productsList...), components, nil)

	rejected := httptest.NewRecorder()
	h.AddBundleComponent().ServeHTTP(rejected, compositionRequest(http.MethodPost, "/x", map[string]any{
		"component_product_id": "extra",
		"quantity":             1,
		"required":             true,
	}, map[string]string{"id": "b1"}))
	if rejected.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body=%s", rejected.Code, rejected.Body.String())
	}
	if components.upsert != nil || len(components.list) != catalog.MaxBundleRequiredComponents {
		t.Fatalf("committed upsert=%+v rows=%d", components.upsert, len(components.list))
	}
	for _, row := range components.list {
		if row.ComponentProductID == "extra" {
			t.Fatal("51st required component was committed")
		}
	}

	updated := httptest.NewRecorder()
	h.AddBundleComponent().ServeHTTP(updated, compositionRequest(http.MethodPost, "/x", map[string]any{
		"component_product_id": "c0",
		"quantity":             4,
		"required":             true,
	}, map[string]string{"id": "b1"}))
	if updated.Code != http.StatusOK {
		t.Fatalf("quantity change status = %d body=%s", updated.Code, updated.Body.String())
	}
	if components.upsert == nil || components.upsert.Quantity != 4 || components.upsert.ComponentProductID != "c0" {
		t.Fatalf("committed upsert = %+v", components.upsert)
	}
	if len(components.list) != catalog.MaxBundleRequiredComponents {
		t.Fatalf("committed rows = %d", len(components.list))
	}
}

func TestProductCompositionAdmin_AddGroupedMember_Validation(t *testing.T) {
	products := compositionProducts(
		&catalog.Product{ID: "s1", Type: catalog.TypeSimple, Status: catalog.StatusActive},
		&catalog.Product{ID: "g1", Type: catalog.TypeGrouped, Status: catalog.StatusActive},
		&catalog.Product{ID: "m1", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	)
	h := newCompositionHandler(products, nil, &compGroupedRepo{})

	missing := httptest.NewRecorder()
	h.AddGroupedMember().ServeHTTP(missing, compositionRequest(http.MethodPost, "/x", map[string]any{}, map[string]string{"id": "g1"}))
	if missing.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing member status = %d body=%s", missing.Code, missing.Body.String())
	}
	_, msg := decodeError(t, missing)
	if !bytes.Contains([]byte(msg), []byte("member product id")) {
		t.Fatalf("message = %q", msg)
	}

	empty := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(nil))
	req.SetPathValue("id", "g1")
	h.AddGroupedMember().ServeHTTP(empty, req)
	if empty.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty body status = %d", empty.Code)
	}

	wrong := httptest.NewRecorder()
	h.AddGroupedMember().ServeHTTP(wrong, compositionRequest(http.MethodPost, "/x", map[string]any{
		"member_product_id": "m1",
	}, map[string]string{"id": "s1"}))
	if wrong.Code != http.StatusUnprocessableEntity {
		t.Fatalf("wrong parent status = %d body=%s", wrong.Code, wrong.Body.String())
	}
}

func TestProductCompositionAdmin_RemoveGroupedMember_WrongParentType(t *testing.T) {
	products := compositionProducts(&catalog.Product{ID: "b1", Type: catalog.TypeBundle, Status: catalog.StatusActive})
	members := &compGroupedRepo{}
	h := newCompositionHandler(products, nil, members)
	rec := httptest.NewRecorder()
	h.RemoveGroupedMember().ServeHTTP(rec, compositionRequest(http.MethodDelete, "/x", nil, map[string]string{
		"id": "b1", "memberId": "m1",
	}))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if members.removed.memberID != "" {
		t.Fatalf("remove called for wrong parent type: %+v", members.removed)
	}
}

func TestProductCompositionAdmin_MuxPermissions(t *testing.T) {
	products := compositionProducts(
		&catalog.Product{ID: "b1", Type: catalog.TypeBundle, Status: catalog.StatusActive, Name: "Kit", Slug: "kit"},
		&catalog.Product{ID: "c1", Type: catalog.TypeSimple, Status: catalog.StatusActive, Name: "Leaf", Slug: "leaf"},
		&catalog.Product{ID: "g1", Type: catalog.TypeGrouped, Status: catalog.StatusActive, Name: "Group", Slug: "group"},
	)
	h := newCompositionHandler(products, &compBundleRepo{}, &compGroupedRepo{})
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/products/{id}/bundle-components", admin.RequirePermission(rbac.ProductsRead)(h.ListBundleComponents()))
	mux.Handle("POST /api/v1/admin/products/{id}/bundle-components", admin.RequirePermission(rbac.ProductsWrite)(h.AddBundleComponent()))
	mux.Handle("DELETE /api/v1/admin/products/{id}/bundle-components/{componentId}", admin.RequirePermission(rbac.ProductsWrite)(h.RemoveBundleComponent()))
	mux.Handle("GET /api/v1/admin/products/{id}/grouped-members", admin.RequirePermission(rbac.ProductsRead)(h.ListGroupedMembers()))
	mux.Handle("POST /api/v1/admin/products/{id}/grouped-members", admin.RequirePermission(rbac.ProductsWrite)(h.AddGroupedMember()))
	mux.Handle("DELETE /api/v1/admin/products/{id}/grouped-members/{memberId}", admin.RequirePermission(rbac.ProductsWrite)(h.RemoveGroupedMember()))

	serve := func(method, target string, body []byte, role identity.Role) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, target, bytes.NewReader(body))
		req = testhelper.AuthenticatedRequest(req, "user-1", role)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	if rec := serve(http.MethodGet, "/api/v1/admin/products/b1/bundle-components", nil, identity.RoleCustomer); rec.Code != http.StatusForbidden {
		t.Fatalf("customer GET = %d, want 403", rec.Code)
	}
	if rec := serve(http.MethodGet, "/api/v1/admin/products/b1/bundle-components", nil, identity.RoleSupport); rec.Code != http.StatusOK {
		t.Fatalf("support GET bundle = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := serve(http.MethodGet, "/api/v1/admin/products/g1/grouped-members", nil, identity.RoleSupport); rec.Code != http.StatusOK {
		t.Fatalf("support GET grouped = %d body=%s", rec.Code, rec.Body.String())
	}
	postBody := []byte(`{"component_product_id":"c1","quantity":1}`)
	if rec := serve(http.MethodPost, "/api/v1/admin/products/b1/bundle-components", postBody, identity.RoleSupport); rec.Code != http.StatusForbidden {
		t.Fatalf("support POST = %d, want 403", rec.Code)
	}
	if rec := serve(http.MethodDelete, "/api/v1/admin/products/b1/bundle-components/c1", nil, identity.RoleSupport); rec.Code != http.StatusForbidden {
		t.Fatalf("support DELETE = %d, want 403", rec.Code)
	}
	if rec := serve(http.MethodPost, "/api/v1/admin/products/g1/grouped-members", []byte(`{"member_product_id":"c1"}`), identity.RoleSupport); rec.Code != http.StatusForbidden {
		t.Fatalf("support grouped POST = %d, want 403", rec.Code)
	}
	if rec := serve(http.MethodDelete, "/api/v1/admin/products/g1/grouped-members/c1", nil, identity.RoleSupport); rec.Code != http.StatusForbidden {
		t.Fatalf("support grouped DELETE = %d, want 403", rec.Code)
	}
	if rec := serve(http.MethodPost, "/api/v1/admin/products/b1/bundle-components", postBody, identity.RoleEditor); rec.Code != http.StatusOK {
		t.Fatalf("editor POST = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := serve(http.MethodDelete, "/api/v1/admin/products/b1/bundle-components/c1", nil, identity.RoleEditor); rec.Code != http.StatusOK {
		t.Fatalf("editor DELETE = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestProductCompositionAdmin_ListBundleComponents_MissingParent(t *testing.T) {
	h := newCompositionHandler(compositionProducts(), nil, nil)
	rec := httptest.NewRecorder()
	h.ListBundleComponents().ServeHTTP(rec, compositionRequest(http.MethodGet, "/x", nil, map[string]string{"id": "missing"}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestProductCompositionAdmin_AddBundleComponent_EmitsProductUpdatedEvent
// pins PR-1080: adding a component publishes catalog.EventProductUpdated
// for the parent so IndexUpdateSubscriber refreshes rollup visibility.
func TestProductCompositionAdmin_AddBundleComponent_EmitsProductUpdatedEvent(t *testing.T) {
	products := compositionProducts(
		&catalog.Product{ID: "b1", Type: catalog.TypeBundle, Status: catalog.StatusActive, Name: "Kit", Slug: "kit"},
		&catalog.Product{ID: "c1", Type: catalog.TypeSimple, Status: catalog.StatusActive, Name: "Leaf", Slug: "leaf"},
	)
	h := newCompositionHandler(products, &compBundleRepo{}, nil)
	bus := event.NewBus(logger.New("error"))
	h.SetBus(bus)
	asyncRan := make(chan catalog.ProductUpdatedData, 1)
	bus.OnAsync(catalog.EventProductUpdated, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(catalog.ProductUpdatedData)
		if !ok {
			t.Errorf("data type %T", evt.Data)
			return nil
		}
		asyncRan <- data
		return nil
	})
	rec := httptest.NewRecorder()
	h.AddBundleComponent().ServeHTTP(rec, compositionRequest(http.MethodPost, "/x", map[string]any{
		"component_product_id": "c1",
		"quantity":             1,
	}, map[string]string{"id": "b1"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	select {
	case data := <-asyncRan:
		if data.ProductID != "b1" || data.Slug != "kit" || data.Name != "Kit" || data.Status != catalog.StatusActive {
			t.Fatalf("data = %+v", data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected async ProductUpdated for parent bundle")
	}
}

// TestProductCompositionAdmin_RemoveBundleComponent_EmitsProductUpdatedEvent
// is Add's counterpart for Remove.
func TestProductCompositionAdmin_RemoveBundleComponent_EmitsProductUpdatedEvent(t *testing.T) {
	products := compositionProducts(
		&catalog.Product{ID: "b1", Type: catalog.TypeBundle, Status: catalog.StatusActive, Name: "Kit", Slug: "kit"},
	)
	components := &compBundleRepo{list: []catalog.BundleComponent{{
		BundleProductID: "b1", ComponentProductID: "c1", Quantity: 1, Required: true,
	}}}
	h := newCompositionHandler(products, components, nil)
	bus := event.NewBus(logger.New("error"))
	h.SetBus(bus)
	asyncRan := make(chan string, 1)
	bus.OnAsync(catalog.EventProductUpdated, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(catalog.ProductUpdatedData)
		if ok {
			asyncRan <- data.ProductID
		}
		return nil
	})
	rec := httptest.NewRecorder()
	h.RemoveBundleComponent().ServeHTTP(rec, compositionRequest(http.MethodDelete, "/x", nil, map[string]string{
		"id": "b1", "componentId": "c1",
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	select {
	case id := <-asyncRan:
		if id != "b1" {
			t.Fatalf("product id = %s, want b1", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected async ProductUpdated for parent bundle")
	}
}

// TestProductCompositionAdmin_AddBundleComponent_NoBusStillSucceeds pins
// that SetBus is optional — nil bus must not break composition writes.
func TestProductCompositionAdmin_AddBundleComponent_NoBusStillSucceeds(t *testing.T) {
	products := compositionProducts(
		&catalog.Product{ID: "b1", Type: catalog.TypeBundle, Status: catalog.StatusActive},
		&catalog.Product{ID: "c1", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	)
	h := newCompositionHandler(products, &compBundleRepo{}, nil)
	rec := httptest.NewRecorder()
	h.AddBundleComponent().ServeHTTP(rec, compositionRequest(http.MethodPost, "/x", map[string]any{
		"component_product_id": "c1",
		"quantity":             1,
	}, map[string]string{"id": "b1"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestProductCompositionAdmin_AddGroupedMember_NoProductUpdatedEvent pins
// that grouped membership is display-only and does not publish.
func TestProductCompositionAdmin_AddGroupedMember_NoProductUpdatedEvent(t *testing.T) {
	products := compositionProducts(
		&catalog.Product{ID: "g1", Type: catalog.TypeGrouped, Status: catalog.StatusActive, Name: "Group", Slug: "group"},
		&catalog.Product{ID: "m1", Type: catalog.TypeSimple, Status: catalog.StatusActive, Name: "Member", Slug: "member"},
	)
	h := newCompositionHandler(products, nil, &compGroupedRepo{})
	bus := event.NewBus(logger.New("error"))
	h.SetBus(bus)
	published := false
	bus.On(catalog.EventProductUpdated, func(context.Context, event.Event) error {
		published = true
		return nil
	})
	rec := httptest.NewRecorder()
	h.AddGroupedMember().ServeHTTP(rec, compositionRequest(http.MethodPost, "/x", map[string]any{
		"member_product_id": "m1",
	}, map[string]string{"id": "g1"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if published {
		t.Fatal("grouped membership must not publish ProductUpdated")
	}
}
