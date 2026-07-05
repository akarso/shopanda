package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/application/admin"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/apperror"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

type mockExtensionFieldRepo struct {
	fields map[string]domainext.ExtensionField
}

func newMockExtensionFieldRepo() *mockExtensionFieldRepo {
	return &mockExtensionFieldRepo{fields: make(map[string]domainext.ExtensionField)}
}

func (m *mockExtensionFieldRepo) Save(_ context.Context, field domainext.ExtensionField) error {
	m.fields[field.Code] = field
	return nil
}

func (m *mockExtensionFieldRepo) FindByCode(_ context.Context, code string) (domainext.ExtensionField, error) {
	field, ok := m.fields[code]
	if !ok {
		return domainext.ExtensionField{}, apperror.NotFound("extension field not found")
	}
	return field, nil
}

func (m *mockExtensionFieldRepo) ListActive(_ context.Context, scope domainext.TargetType) ([]domainext.ExtensionField, error) {
	out := make([]domainext.ExtensionField, 0)
	for _, field := range m.fields {
		if scope != "" && field.Scope != scope {
			continue
		}
		out = append(out, field)
	}
	return out, nil
}

func (m *mockExtensionFieldRepo) SoftDelete(_ context.Context, code string) error {
	if _, ok := m.fields[code]; !ok {
		return apperror.NotFound("extension field not found")
	}
	delete(m.fields, code)
	return nil
}

func newExtensionFieldAdminRouter(h *shophttp.ExtensionFieldAdminHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/extensions/fields", h.ListFields())
	mux.HandleFunc("GET /api/v1/admin/extensions/fields/{code}", h.GetField())
	mux.HandleFunc("POST /api/v1/admin/extensions/fields", h.CreateField())
	mux.HandleFunc("PUT /api/v1/admin/extensions/fields/{code}", h.UpdateField())
	mux.HandleFunc("DELETE /api/v1/admin/extensions/fields/{code}", h.DeleteField())
	return mux
}

func extensionFieldBody(t *testing.T, payload map[string]interface{}) *bytes.Buffer {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return bytes.NewBuffer(raw)
}

func withAdminPermissions(perms ...string) context.Context {
	return (&admin.AdminContext{
		AdminID:     "admin-1",
		Permissions: perms,
	}).WithContext(context.Background())
}

func TestExtensionFieldAdminHandler_CreateAndGet(t *testing.T) {
	reg := extensionapp.NewRegistry()
	repo := newMockExtensionFieldRepo()
	h := shophttp.NewExtensionFieldAdminHandler(extensionapp.NewFieldService(reg, repo))
	router := newExtensionFieldAdminRouter(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/extensions/fields", extensionFieldBody(t, map[string]interface{}{
		"code":  "acme.gift.wrap_level",
		"label": "Gift wrap",
		"type":  "enum",
		"scope": "product",
		"validation": map[string]interface{}{
			"options": []string{"none", "standard"},
		},
	}))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/admin/extensions/fields/acme.gift.wrap_level", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestExtensionFieldAdminHandler_CreateDuplicateConflict(t *testing.T) {
	reg := extensionapp.NewRegistry()
	repo := newMockExtensionFieldRepo()
	h := shophttp.NewExtensionFieldAdminHandler(extensionapp.NewFieldService(reg, repo))
	router := newExtensionFieldAdminRouter(h)
	payload := extensionFieldBody(t, map[string]interface{}{
		"code": "acme.dup.field", "label": "Dup", "type": "string", "scope": "product",
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/admin/extensions/fields", payload))
	if rec.Code != http.StatusCreated {
		t.Fatalf("first create status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("POST", "/api/v1/admin/extensions/fields", extensionFieldBody(t, map[string]interface{}{
		"code": "acme.dup.field", "label": "Dup 2", "type": "string", "scope": "product",
	})))
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d, want %d; body: %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestExtensionFieldAdminHandler_ListFiltersByTargetType(t *testing.T) {
	reg := extensionapp.NewRegistry()
	repo := newMockExtensionFieldRepo()
	svc := extensionapp.NewFieldService(reg, repo)
	h := shophttp.NewExtensionFieldAdminHandler(svc)
	router := newExtensionFieldAdminRouter(h)

	for _, def := range []domainext.FieldDef{
		{Code: "acme.product.field", Label: "Product", Type: domainext.FieldTypeString, Scope: domainext.TargetProduct, StorageMode: domainext.StorageStored},
		{Code: "acme.cart.field", Label: "Cart", Type: domainext.FieldTypeString, Scope: domainext.TargetCartItem, StorageMode: domainext.StorageStored},
	} {
		if _, err := svc.Create(context.Background(), def); err != nil {
			t.Fatalf("Create %s: %v", def.Code, err)
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/extensions/fields?target_type=product", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d; body: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Fields []struct {
				Code string `json:"code"`
			} `json:"fields"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data.Fields) != 1 || resp.Data.Fields[0].Code != "acme.product.field" {
		t.Fatalf("fields = %+v", resp.Data.Fields)
	}
}

func TestExtensionFieldAdminHandler_ListHidesPrivateWithoutCapability(t *testing.T) {
	reg := extensionapp.NewRegistry()
	repo := newMockExtensionFieldRepo()
	svc := extensionapp.NewFieldService(reg, repo)
	h := shophttp.NewExtensionFieldAdminHandler(svc)
	router := newExtensionFieldAdminRouter(h)

	public := domainext.FieldDef{Code: "acme.public.field", Label: "Public", Type: domainext.FieldTypeString, Scope: domainext.TargetProduct, StorageMode: domainext.StorageStored}
	private := domainext.FieldDef{Code: "acme.private.field", Label: "Private", Type: domainext.FieldTypeString, Scope: domainext.TargetProduct, StorageMode: domainext.StorageStored, Visibility: domainext.VisibilityPrivate}
	for _, def := range []domainext.FieldDef{public, private} {
		if _, err := svc.Create(context.Background(), def); err != nil {
			t.Fatalf("Create %s: %v", def.Code, err)
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/extensions/fields?include_private=true", nil)
	req = req.WithContext(withAdminPermissions(string(rbac.ExtensionsRead)))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d; body: %s", rec.Code, rec.Body.String())
	}
	var hiddenResp struct {
		Data struct {
			Fields []struct {
				Code string `json:"code"`
			} `json:"fields"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &hiddenResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(hiddenResp.Data.Fields) != 1 {
		t.Fatalf("without private capability fields = %+v, want public only", hiddenResp.Data.Fields)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/admin/extensions/fields?include_private=true", nil)
	req = req.WithContext(withAdminPermissions(string(rbac.ExtensionsRead), string(rbac.ExtensionsPrivateRead)))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d; body: %s", rec.Code, rec.Body.String())
	}
	var allResp struct {
		Data struct {
			Fields []struct {
				Code string `json:"code"`
			} `json:"fields"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &allResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(allResp.Data.Fields) != 2 {
		t.Fatalf("with private capability fields = %+v, want 2", allResp.Data.Fields)
	}
}

func TestExtensionFieldAdminHandler_CreateValidationError(t *testing.T) {
	reg := extensionapp.NewRegistry()
	repo := newMockExtensionFieldRepo()
	h := shophttp.NewExtensionFieldAdminHandler(extensionapp.NewFieldService(reg, repo))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/extensions/fields", extensionFieldBody(t, map[string]interface{}{
		"code": "not-namespaced", "label": "Bad", "type": "string", "scope": "product",
	}))
	newExtensionFieldAdminRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
}
