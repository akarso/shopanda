package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/auth"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
)

func schemaSetup() (*admin.Registry, *http.ServeMux) {
	reg := admin.NewRegistry()
	reg.RegisterForm("product.form", admin.Form{
		Fields: []admin.Field{
			{Name: "name", Type: "text", Label: "Product Name", Required: true},
			{Name: "status", Type: "select", Label: "Status", Options: []admin.Option{
				{Label: "Draft", Value: "draft"},
				{Label: "Active", Value: "active"},
			}, Default: "draft"},
		},
	})
	reg.RegisterGrid("product.grid", admin.Grid{
		Columns: []admin.Column{
			{Name: "id", Label: "ID"},
			{Name: "name", Label: "Name"},
		},
	})

	handler := shophttp.NewSchemaHandler(reg)
	requireAdmin := shophttp.RequireRole(identity.RoleAdmin)
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/forms/{name}", requireAdmin(handler.GetForm()))
	mux.Handle("GET /api/v1/admin/grids/{name}", requireAdmin(handler.GetGrid()))
	return reg, mux
}

func TestSchemaHandler_GetForm_OK(t *testing.T) {
	_, mux := schemaSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/forms/product.form", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var envelope struct {
		Data struct {
			Form struct {
				Name   string `json:"name"`
				Fields []struct {
					Name     string      `json:"name"`
					Type     string      `json:"type"`
					Label    string      `json:"label"`
					Required bool        `json:"required"`
					Default  interface{} `json:"default"`
					Options  []struct {
						Label string `json:"label"`
						Value string `json:"value"`
					} `json:"options"`
				} `json:"fields"`
			} `json:"form"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	form := envelope.Data.Form
	if form.Name != "product.form" {
		t.Errorf("name = %q, want %q", form.Name, "product.form")
	}
	if len(form.Fields) != 2 {
		t.Fatalf("fields count = %d, want 2", len(form.Fields))
	}
	if form.Fields[0].Name != "name" {
		t.Errorf("field[0].Name = %q, want %q", form.Fields[0].Name, "name")
	}
	if !form.Fields[0].Required {
		t.Error("field[0] should be required")
	}
	if len(form.Fields[1].Options) != 2 {
		t.Fatalf("status options = %d, want 2", len(form.Fields[1].Options))
	}
	if form.Fields[1].Options[0].Value != "draft" {
		t.Errorf("option[0].Value = %q, want %q", form.Fields[1].Options[0].Value, "draft")
	}
}

func TestSchemaHandler_GetForm_NotFound(t *testing.T) {
	_, mux := schemaSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/forms/nonexistent", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestSchemaHandler_GetGrid_OK(t *testing.T) {
	_, mux := schemaSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/grids/product.grid", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var envelope struct {
		Data struct {
			Grid struct {
				Name    string `json:"name"`
				Columns []struct {
					Name  string `json:"name"`
					Label string `json:"label"`
				} `json:"columns"`
			} `json:"grid"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	grid := envelope.Data.Grid
	if grid.Name != "product.grid" {
		t.Errorf("name = %q, want %q", grid.Name, "product.grid")
	}
	if len(grid.Columns) != 2 {
		t.Fatalf("columns count = %d, want 2", len(grid.Columns))
	}
	if grid.Columns[0].Name != "id" {
		t.Errorf("column[0].Name = %q, want %q", grid.Columns[0].Name, "id")
	}
}

func TestSchemaHandler_GetGrid_NotFound(t *testing.T) {
	_, mux := schemaSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/grids/nonexistent", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestSchemaHandler_GetForm_RequiresAdmin(t *testing.T) {
	_, mux := schemaSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/forms/product.form", nil)
	// No admin context set — should be rejected.
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestSchemaHandler_GetGrid_RequiresAdmin(t *testing.T) {
	_, mux := schemaSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/grids/product.grid", nil)
	// No admin context set — should be rejected.
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

// schemaPermSetup creates a registry with permission-gated schemas.
func schemaPermSetup() (*admin.Registry, *http.ServeMux) {
	reg := admin.NewRegistry()
	reg.RegisterForm("product.form", admin.Form{
		Fields: []admin.Field{
			{Name: "name", Type: "text", Label: "Product Name", Required: true},
		},
	})
	reg.RegisterGrid("product.grid", admin.Grid{
		Columns: []admin.Column{
			{Name: "id", Label: "ID"},
		},
	})
	_ = reg.SetFormPermission("product.form", rbac.ProductsWrite)
	_ = reg.SetGridPermission("product.grid", rbac.ProductsRead)

	handler := shophttp.NewSchemaHandler(reg)
	requireAuth := shophttp.RequireAuth()
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/forms/{name}", requireAuth(handler.GetForm()))
	mux.Handle("GET /api/v1/admin/grids/{name}", requireAuth(handler.GetGrid()))
	return reg, mux
}

func TestSchemaHandler_GetForm_PermissionGranted(t *testing.T) {
	_, mux := schemaPermSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/forms/product.form", nil)
	// Editor has products.write.
	req = testhelper.AuthenticatedRequest(req, "editor-1", identity.RoleEditor)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestSchemaHandler_GetForm_PermissionDenied(t *testing.T) {
	_, mux := schemaPermSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/forms/product.form", nil)
	// Support does NOT have products.write.
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestSchemaHandler_GetGrid_PermissionGranted(t *testing.T) {
	_, mux := schemaPermSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/grids/product.grid", nil)
	// Support has products.read.
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestSchemaHandler_GetGrid_PermissionDenied(t *testing.T) {
	_, mux := schemaPermSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/grids/product.grid", nil)
	// Customer has no admin permissions.
	req = testhelper.AuthenticatedRequest(req, "cust-1", identity.RoleCustomer)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

// schemaPermNoAuthSetup mounts the handler WITHOUT RequireAuth middleware
// so that the handler's own guest-detection is exercised.
func schemaPermNoAuthSetup() *http.ServeMux {
	reg := admin.NewRegistry()
	reg.RegisterForm("product.form", admin.Form{
		Fields: []admin.Field{
			{Name: "name", Type: "text", Label: "Product Name"},
		},
	})
	reg.RegisterGrid("product.grid", admin.Grid{
		Columns: []admin.Column{{Name: "id", Label: "ID"}},
	})
	_ = reg.SetFormPermission("product.form", rbac.ProductsWrite)
	_ = reg.SetGridPermission("product.grid", rbac.ProductsRead)

	handler := shophttp.NewSchemaHandler(reg)
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/forms/{name}", handler.GetForm())
	mux.Handle("GET /api/v1/admin/grids/{name}", handler.GetGrid())
	return mux
}

func TestSchemaHandler_GetForm_GuestReturns401(t *testing.T) {
	mux := schemaPermNoAuthSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/forms/product.form", nil)
	ctx := auth.WithIdentity(req.Context(), identity.Guest())
	req = req.WithContext(ctx)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestSchemaHandler_GetGrid_GuestReturns401(t *testing.T) {
	mux := schemaPermNoAuthSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/grids/product.grid", nil)
	ctx := auth.WithIdentity(req.Context(), identity.Guest())
	req = req.WithContext(ctx)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}
