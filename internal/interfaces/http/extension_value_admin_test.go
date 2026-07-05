package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/apperror"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

type mockExtensionValueRepo struct {
	values map[string]domainext.Value
}

func valueStoreKey(target domainext.Target, fieldCode string) string {
	return string(target.Type) + ":" + target.ID + ":" + fieldCode
}

func newMockExtensionValueRepo() *mockExtensionValueRepo {
	return &mockExtensionValueRepo{values: make(map[string]domainext.Value)}
}

func (m *mockExtensionValueRepo) ListByTarget(_ context.Context, target domainext.Target) ([]domainext.Value, error) {
	out := make([]domainext.Value, 0)
	for _, value := range m.values {
		if value.TargetType == target.Type && value.TargetID == target.ID {
			out = append(out, value)
		}
	}
	return out, nil
}

func (m *mockExtensionValueRepo) Upsert(_ context.Context, value domainext.Value) error {
	m.values[valueStoreKey(domainext.Target{Type: value.TargetType, ID: value.TargetID}, value.FieldCode)] = value
	return nil
}

func (m *mockExtensionValueRepo) Delete(_ context.Context, target domainext.Target, fieldCode string) error {
	key := valueStoreKey(target, fieldCode)
	if _, ok := m.values[key]; !ok {
		return apperror.NotFound("extension value not found")
	}
	delete(m.values, key)
	return nil
}

func registerExtensionField(t *testing.T, reg *extensionapp.Registry, def domainext.FieldDef) {
	t.Helper()
	if err := reg.Register(def); err != nil {
		t.Fatalf("register field %q: %v", def.Code, err)
	}
}

func newExtensionValueAdminRouter(h *shophttp.ExtensionValueAdminHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/extensions/values/{targetType}/{targetID}", h.ListValues())
	mux.HandleFunc("PUT /api/v1/admin/extensions/values/{targetType}/{targetID}", h.PutValues())
	mux.HandleFunc("DELETE /api/v1/admin/extensions/values/{targetType}/{targetID}/{fieldCode}", h.DeleteValue())
	mux.HandleFunc("GET /api/v1/admin/products/{id}/extensions", h.ListProductExtensions())
	mux.HandleFunc("PUT /api/v1/admin/products/{id}/extensions", h.PutProductExtensions())
	return mux
}

func extensionValueBody(t *testing.T, payload map[string]interface{}) *bytes.Buffer {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return bytes.NewBuffer(raw)
}

func decodeExtensionValueResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var envelope struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v; body: %s", err, rec.Body.String())
	}
	return envelope.Data
}

func decodeExtensionValueError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error: %v; body: %s", err, rec.Body.String())
	}
	return envelope.Error.Code
}

func setupExtensionValueAdmin(t *testing.T) (*shophttp.ExtensionValueAdminHandler, *http.ServeMux) {
	t.Helper()
	reg := extensionapp.NewRegistry()
	registerExtensionField(t, reg, domainext.FieldDef{
		Code:        "acme.note",
		Label:       "Note",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetProduct,
		StorageMode: domainext.StorageStored,
	})
	registerExtensionField(t, reg, domainext.FieldDef{
		Code:        "acme.secret",
		Label:       "Secret",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetProduct,
		StorageMode: domainext.StorageStored,
		Visibility:  domainext.VisibilityPrivate,
	})
	svc := extensionapp.NewValueService(reg, newMockExtensionValueRepo())
	h := shophttp.NewExtensionValueAdminHandler(svc)
	return h, newExtensionValueAdminRouter(h)
}

func TestExtensionValueAdminHandler_PutAndGet(t *testing.T) {
	_, router := setupExtensionValueAdmin(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/extensions/values/product/prod-1", extensionValueBody(t, map[string]interface{}{
		"values": []map[string]interface{}{
			{"field_code": "acme.note", "value": "hello"},
		},
	}))
	req = req.WithContext(withAdminPermissions(string(rbac.ExtensionsWrite)))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/admin/extensions/values/product/prod-1", nil)
	req = req.WithContext(withAdminPermissions(string(rbac.ExtensionsRead)))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeExtensionValueResponse(t, rec)
	values, ok := data["values"].([]interface{})
	if !ok || len(values) != 1 {
		t.Fatalf("values = %#v", data["values"])
	}
}

func TestExtensionValueAdminHandler_PrivateFieldWriteForbidden(t *testing.T) {
	_, router := setupExtensionValueAdmin(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/extensions/values/product/prod-1", extensionValueBody(t, map[string]interface{}{
		"values": []map[string]interface{}{
			{"field_code": "acme.secret", "value": "hidden"},
		},
	}))
	req = req.WithContext(withAdminPermissions(string(rbac.ExtensionsWrite)))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("put status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if code := decodeExtensionValueError(t, rec); code != string(apperror.CodeForbiddenPrivateField) {
		t.Fatalf("error code = %q, want %q", code, apperror.CodeForbiddenPrivateField)
	}
}

func TestExtensionValueAdminHandler_UnknownFieldCode(t *testing.T) {
	_, router := setupExtensionValueAdmin(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/extensions/values/product/prod-1", extensionValueBody(t, map[string]interface{}{
		"values": []map[string]interface{}{
			{"field_code": "missing.field", "value": "x"},
		},
	}))
	req = req.WithContext(withAdminPermissions(string(rbac.ExtensionsWrite)))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("put status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if code := decodeExtensionValueError(t, rec); code != string(apperror.CodeUnknownFieldCode) {
		t.Fatalf("error code = %q, want %q", code, apperror.CodeUnknownFieldCode)
	}
}

func TestExtensionValueAdminHandler_ProductAlias(t *testing.T) {
	_, router := setupExtensionValueAdmin(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/prod-2/extensions", extensionValueBody(t, map[string]interface{}{
		"values": []map[string]interface{}{
			{"field_code": "acme.note", "value": "via alias"},
		},
	}))
	req = req.WithContext(withAdminPermissions(string(rbac.ExtensionsWrite)))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put alias status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/admin/products/prod-2/extensions", nil)
	req = req.WithContext(withAdminPermissions(string(rbac.ExtensionsRead)))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get alias status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestExtensionValueAdminHandler_Delete(t *testing.T) {
	_, router := setupExtensionValueAdmin(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/extensions/values/product/prod-3", extensionValueBody(t, map[string]interface{}{
		"values": []map[string]interface{}{
			{"field_code": "acme.note", "value": "delete me"},
		},
	}))
	req = req.WithContext(withAdminPermissions(string(rbac.ExtensionsWrite)))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/api/v1/admin/extensions/values/product/prod-3/acme.note", nil)
	req = req.WithContext(withAdminPermissions(string(rbac.ExtensionsWrite)))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestExtensionValueAdminHandler_PrivateReadRequiresPermission(t *testing.T) {
	reg := extensionapp.NewRegistry()
	registerExtensionField(t, reg, domainext.FieldDef{
		Code:        "acme.secret",
		Label:       "Secret",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetProduct,
		StorageMode: domainext.StorageStored,
		Visibility:  domainext.VisibilityPrivate,
	})
	svc := extensionapp.NewValueService(reg, newMockExtensionValueRepo())
	if _, err := svc.UpsertBatch(context.Background(), domainext.Target{Type: domainext.TargetProduct, ID: "prod-4"}, []domainext.ValueInput{
		{FieldCode: "acme.secret", Value: "hidden"},
	}, "admin-1", true); err != nil {
		t.Fatalf("UpsertBatch: %v", err)
	}
	h := shophttp.NewExtensionValueAdminHandler(svc)
	router := newExtensionValueAdminRouter(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/extensions/values/product/prod-4?include_private=true", nil)
	req = req.WithContext(withAdminPermissions(string(rbac.ExtensionsRead)))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get without private perm status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeExtensionValueResponse(t, rec)
	values, _ := data["values"].([]interface{})
	if len(values) != 0 {
		t.Fatalf("expected private values hidden, got %#v", values)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/admin/extensions/values/product/prod-4?include_private=true", nil)
	req = req.WithContext(withAdminPermissions(string(rbac.ExtensionsRead), string(rbac.ExtensionsPrivateRead)))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get with private perm status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data = decodeExtensionValueResponse(t, rec)
	values, _ = data["values"].([]interface{})
	if len(values) != 1 {
		t.Fatalf("expected one private value, got %#v", values)
	}
}
