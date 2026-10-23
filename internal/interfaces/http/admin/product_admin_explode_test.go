package admin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/interfaces/http/admin"
)

func TestProductAdminHandler_Create_ExplodeOrderLinesRequiresDynamicModes(t *testing.T) {
	var created *catalog.Product
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, p *catalog.Product) error {
			created = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithVariantPrices(adminOwnPriceVariants{}, adminOwnPrices{}).
		WithVariantStock(adminOwnStock{})

	body := jsonBody(t, map[string]interface{}{
		"name":                "Kit",
		"slug":                "kit",
		"type":                "bundle",
		"pricing_mode":        "dynamic",
		"stock_mode":          "dynamic",
		"explode_order_lines": true,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", body)
	newAdminRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	if created == nil || !created.ExplodeOrderLines {
		t.Fatalf("created = %+v, want explode true", created)
	}

	body = jsonBody(t, map[string]interface{}{
		"name":                "Kit",
		"slug":                "kit-fixed",
		"type":                "bundle",
		"pricing_mode":        "fixed",
		"stock_mode":          "dynamic",
		"explode_order_lines": true,
	})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/v1/admin/products", body)
	newAdminRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), catalog.ExplodeOrderLinesRequiresDynamicMessage) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestProductAdminHandler_Update_ExplodeWrongModes422(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{
				ID: id, Name: "Kit", Slug: "kit", Status: catalog.StatusDraft,
				Type: catalog.TypeBundle, PricingMode: catalog.PricingModeFixed, StockMode: catalog.StockModeFixed,
				VisibilityModes: catalog.DefaultVisibilityAxes(),
			}, nil
		},
		updateFn: func(_ context.Context, _ *catalog.Product) error {
			t.Fatal("Update must not run")
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).WithTxStarter(adminFakeTxDBConn())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", jsonBody(t, map[string]interface{}{
		"explode_order_lines": true,
	}))
	newAdminRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), catalog.ExplodeOrderLinesRequiresDynamicMessage) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestProductAdminHandler_Update_TypeChangeClearsExplodeOrderLines(t *testing.T) {
	existing := &catalog.Product{
		ID: "p1", Name: "Kit", Slug: "kit", Status: catalog.StatusDraft,
		Type: catalog.TypeBundle, PricingMode: catalog.PricingModeDynamic, StockMode: catalog.StockModeDynamic,
		ExplodeOrderLines: true,
		VisibilityModes:   catalog.DefaultVisibilityAxes(),
	}
	var updated *catalog.Product
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			cp := *existing
			return &cp, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			updated = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithBundleComponents(&mockBundleComponentGuard{}).
		WithGroupedMembers(&mockGroupedMemberGuard{}).
		WithTxStarter(adminFakeTxDBConn())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", jsonBody(t, map[string]interface{}{"type": "simple"}))
	newAdminRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if updated == nil || updated.Type != catalog.TypeSimple || updated.ExplodeOrderLines {
		t.Fatalf("updated = %+v, want simple with explode cleared", updated)
	}
	if updated.PricingMode != catalog.PricingModeFixed || updated.StockMode != catalog.StockModeFixed {
		t.Fatalf("modes = %s/%s, want fixed/fixed", updated.PricingMode, updated.StockMode)
	}
}

func TestProductAdminHandler_Update_StockModeChangeClearsOmittedExplode(t *testing.T) {
	existing := &catalog.Product{
		ID: "p1", Name: "Kit", Slug: "kit", Status: catalog.StatusDraft,
		Type: catalog.TypeBundle, PricingMode: catalog.PricingModeDynamic, StockMode: catalog.StockModeDynamic,
		ExplodeOrderLines: true,
		VisibilityModes:   catalog.DefaultVisibilityAxes(),
	}
	var updated *catalog.Product
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			cp := *existing
			return &cp, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			updated = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).WithTxStarter(adminFakeTxDBConn())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", jsonBody(t, map[string]interface{}{
		"stock_mode": "fixed",
	}))
	newAdminRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if updated == nil || updated.Type != catalog.TypeBundle || updated.StockMode != catalog.StockModeFixed || updated.ExplodeOrderLines {
		t.Fatalf("updated = %+v, want bundle, fixed stock, explode false", updated)
	}
	if updated.PricingMode != catalog.PricingModeDynamic {
		t.Fatalf("pricing = %s, want dynamic", updated.PricingMode)
	}
}
