package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	domainAdmin "github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/config"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

type mockConfigRepoForAttrAdmin struct {
	store map[string]interface{}
}

func newMockConfigRepoForAttrAdmin() *mockConfigRepoForAttrAdmin {
	return &mockConfigRepoForAttrAdmin{store: make(map[string]interface{})}
}

func (m *mockConfigRepoForAttrAdmin) Get(_ context.Context, key string) (interface{}, error) {
	return m.store[key], nil
}

func (m *mockConfigRepoForAttrAdmin) Set(_ context.Context, key string, value interface{}) error {
	m.store[key] = value
	return nil
}

func (m *mockConfigRepoForAttrAdmin) SetMany(_ context.Context, entries map[string]interface{}) error {
	for key, value := range entries {
		m.store[key] = value
	}
	return nil
}

func (m *mockConfigRepoForAttrAdmin) Delete(_ context.Context, key string) error {
	delete(m.store, key)
	return nil
}

func (m *mockConfigRepoForAttrAdmin) All(_ context.Context) ([]config.Entry, error) {
	entries := make([]config.Entry, 0, len(m.store))
	for key, value := range m.store {
		entries = append(entries, config.Entry{Key: key, Value: value})
	}
	return entries, nil
}

func newAttributeAdminRouter(h *shophttp.AttributeAdminHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/attributes", h.ListAttributes())
	mux.HandleFunc("GET /api/v1/admin/attributes/{code}", h.GetAttribute())
	mux.HandleFunc("POST /api/v1/admin/attributes", h.CreateAttribute())
	mux.HandleFunc("PUT /api/v1/admin/attributes/{code}", h.UpdateAttribute())
	mux.HandleFunc("DELETE /api/v1/admin/attributes/{code}", h.DeleteAttribute())
	mux.HandleFunc("GET /api/v1/admin/attribute-groups", h.ListGroups())
	mux.HandleFunc("GET /api/v1/admin/attribute-groups/{code}", h.GetGroup())
	mux.HandleFunc("POST /api/v1/admin/attribute-groups", h.CreateGroup())
	mux.HandleFunc("PUT /api/v1/admin/attribute-groups/{code}", h.UpdateGroup())
	mux.HandleFunc("DELETE /api/v1/admin/attribute-groups/{code}", h.DeleteGroup())
	return mux
}

func attributeBody(t *testing.T, payload map[string]interface{}) *bytes.Buffer {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return bytes.NewBuffer(raw)
}

func TestAttributeAdminHandler_CreateAndGet(t *testing.T) {
	store := adminApp.NewAttributeStore(newMockConfigRepoForAttrAdmin())
	h := shophttp.NewAttributeAdminHandler(store)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/attributes", attributeBody(t, map[string]interface{}{
		"code":  "material",
		"label": "Material",
		"type":  "text",
	}))
	newAttributeAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/admin/attributes/material", nil)
	newAttributeAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Attribute struct {
				Code  string `json:"code"`
				Label string `json:"label"`
			} `json:"attribute"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Data.Attribute.Code != "material" || resp.Data.Attribute.Label != "Material" {
		t.Fatalf("attribute = %+v", resp.Data.Attribute)
	}
}

func TestAttributeAdminHandler_CreateDuplicateRejected(t *testing.T) {
	store := adminApp.NewAttributeStore(newMockConfigRepoForAttrAdmin())
	h := shophttp.NewAttributeAdminHandler(store)
	router := newAttributeAdminRouter(h)
	payload := attributeBody(t, map[string]interface{}{
		"code": "color", "label": "Color", "type": "text",
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/admin/attributes", payload))
	if rec.Code != http.StatusCreated {
		t.Fatalf("first create status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/admin/attributes", attributeBody(t, map[string]interface{}{
		"code": "color", "label": "Colour", "type": "text",
	})))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("duplicate status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
}

func TestAttributeAdminHandler_CreateSelectWithoutOptionsRejected(t *testing.T) {
	store := adminApp.NewAttributeStore(newMockConfigRepoForAttrAdmin())
	h := shophttp.NewAttributeAdminHandler(store)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/attributes", attributeBody(t, map[string]interface{}{
		"code": "size", "label": "Size", "type": "select",
	}))
	newAttributeAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
}

func TestAttributeAdminHandler_CreateGroupRequiresKnownAttributes(t *testing.T) {
	store := adminApp.NewAttributeStore(newMockConfigRepoForAttrAdmin())
	h := shophttp.NewAttributeAdminHandler(store)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/attribute-groups", attributeBody(t, map[string]interface{}{
		"code": "apparel", "label": "Apparel", "attributes": []string{"missing"},
	}))
	newAttributeAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
}

func TestAttributeAdminHandler_GroupLifecycle(t *testing.T) {
	store := adminApp.NewAttributeStore(newMockConfigRepoForAttrAdmin())
	h := shophttp.NewAttributeAdminHandler(store)
	router := newAttributeAdminRouter(h)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/admin/attributes", attributeBody(t, map[string]interface{}{
		"code": "color", "label": "Color", "type": "select", "options": []string{"red"},
	})))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create attribute status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/admin/attribute-groups", attributeBody(t, map[string]interface{}{
		"code": "apparel", "label": "Apparel", "attributes": []string{"color"},
	})))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create group status = %d; body: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/admin/attributes?group=apparel", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list by group status = %d", rec.Code)
	}
	var listResp struct {
		Data struct {
			Attributes []struct {
				Code string `json:"code"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Data.Attributes) != 1 || listResp.Data.Attributes[0].Code != "color" {
		t.Fatalf("filtered attributes = %+v", listResp.Data.Attributes)
	}
}

func TestAttributeAdminHandler_ForbiddenWithoutCategoriesPermission(t *testing.T) {
	store := adminApp.NewAttributeStore(newMockConfigRepoForAttrAdmin())
	h := shophttp.NewAttributeAdminHandler(store)
	requireCategoriesRead := shophttp.RequirePermission(rbac.CategoriesRead)
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/attributes", requireCategoriesRead(h.ListAttributes()))

	rec := httptest.NewRecorder()
	req := testhelper.CustomerRequest(httptest.NewRequest("GET", "/api/v1/admin/attributes", nil), "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestSchemaHandler_ProductFormIncludesAttributes(t *testing.T) {
	repo := newMockConfigRepoForAttrAdmin()
	store := adminApp.NewAttributeStore(repo)
	ctx := context.Background()
	if err := store.CreateAttribute(ctx, catalog.Attribute{
		Code: "fabric", Label: "Fabric", Type: catalog.AttributeTypeText,
	}); err != nil {
		t.Fatalf("CreateAttribute: %v", err)
	}

	registry := domainAdmin.NewRegistry()
	adminApp.RegisterProductSchemas(registry)
	handler := shophttp.NewSchemaHandler(registry, store)
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/forms/{name}", handler.GetForm())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/forms/product.form", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"name":"fabric"`)) {
		t.Fatalf("expected fabric field in schema, body: %s", rec.Body.String())
	}
}
