package admin_test

import (
	"context"
	"database/sql"
	"github.com/akarso/shopanda/internal/interfaces/http/admin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/application/exporter"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/legal"
	"github.com/akarso/shopanda/internal/domain/rbac"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

type eprExportProductRepo struct {
	products []catalog.Product
}

func (m *eprExportProductRepo) FindByID(context.Context, string) (*catalog.Product, error) {
	return nil, nil
}
func (m *eprExportProductRepo) FindBySlug(context.Context, string) (*catalog.Product, error) {
	return nil, nil
}
func (m *eprExportProductRepo) List(_ context.Context, offset, limit int) ([]catalog.Product, error) {
	if offset >= len(m.products) {
		return nil, nil
	}
	end := offset + limit
	if end > len(m.products) {
		end = len(m.products)
	}
	return m.products[offset:end], nil
}
func (m *eprExportProductRepo) FindByCategoryID(context.Context, string, int, int) ([]catalog.Product, error) {
	return nil, nil
}
func (m *eprExportProductRepo) Create(context.Context, *catalog.Product) error { return nil }
func (m *eprExportProductRepo) Update(context.Context, *catalog.Product) error { return nil }

type eprExportVariantRepo struct {
	variants map[string][]catalog.Variant
}

func (m *eprExportVariantRepo) FindByID(context.Context, string) (*catalog.Variant, error) {
	return nil, nil
}
func (m *eprExportVariantRepo) FindBySKU(context.Context, string) (*catalog.Variant, error) {
	return nil, nil
}
func (m *eprExportVariantRepo) FindBySKUs(context.Context, []string) (map[string]*catalog.Variant, error) {
	return map[string]*catalog.Variant{}, nil
}
func (m *eprExportVariantRepo) WithTx(*sql.Tx) catalog.VariantRepository { return m }
func (m *eprExportVariantRepo) ListByProductID(_ context.Context, productID string, offset, limit int) ([]catalog.Variant, error) {
	all := m.variants[productID]
	if offset >= len(all) {
		return nil, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}
func (m *eprExportVariantRepo) ListByProductIDs(context.Context, []string, int) (map[string][]catalog.Variant, error) {
	return nil, nil
}
func (m *eprExportVariantRepo) Create(context.Context, *catalog.Variant) error { return nil }
func (m *eprExportVariantRepo) Update(context.Context, *catalog.Variant) error { return nil }

func TestEprReportHandler_ExportCSV(t *testing.T) {
	prodRepo := &eprExportProductRepo{
		products: []catalog.Product{{
			ID:   "p1",
			Name: "Cable",
			Slug: "usb-c-cable",
			Attributes: map[string]interface{}{
				legal.AttrEprPackagingMaterial: "paper_cardboard",
			},
		}},
	}
	varRepo := &eprExportVariantRepo{
		variants: map[string][]catalog.Variant{
			"p1": {{ID: "v1", ProductID: "p1", SKU: "USBC-1M", Attributes: map[string]interface{}{
				legal.AttrEprPackagingWeightG: 12,
			}}},
		},
	}
	exp := exporter.NewEprExporter(prodRepo, varRepo, nil)
	h := admin.NewEprReportHandler(exp)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reports/epr", nil)
	h.Export()(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Fatalf("content-type = %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "USBC-1M") || !strings.Contains(body, "paper_cardboard") {
		t.Fatalf("unexpected csv: %s", body)
	}
}

func TestEprReportHandler_UsesAdminStoreScope(t *testing.T) {
	cfg := stubEprReportConfig{
		legal.ScopedConfigKey("store-de", legal.EprSchemeRegistrationConfigKey):    "DE-LUCID-ADMIN",
		legal.ScopedConfigKey("other-store", legal.EprSchemeRegistrationConfigKey): "OTHER-SCHEME",
	}
	prodRepo := &eprExportProductRepo{
		products: []catalog.Product{{
			ID:   "p1",
			Name: "Shirt",
			Slug: "tshirt",
		}},
	}
	varRepo := &eprExportVariantRepo{
		variants: map[string][]catalog.Variant{
			"p1": {{ID: "v1", ProductID: "p1", SKU: "TSHIRT-M"}},
		},
	}
	exp := exporter.NewEprExporter(prodRepo, varRepo, cfg)
	h := admin.NewEprReportHandler(exp)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reports/epr?store_id=other-store", nil)
	req = req.WithContext((&adminapp.AdminContext{StoreID: "store-de"}).WithContext(req.Context()))
	h.Export()(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "DE-LUCID-ADMIN") {
		t.Fatalf("expected admin context store scope in csv, got: %s", body)
	}
	if strings.Contains(body, "OTHER-SCHEME") {
		t.Fatalf("query store_id must not override admin context: %s", body)
	}
}

type stubEprReportConfig map[string]interface{}

func (s stubEprReportConfig) Get(_ context.Context, key string) (interface{}, error) {
	v, ok := s[key]
	if !ok {
		return nil, nil
	}
	return v, nil
}

func TestEprReportHandler_RequiresProductsRead(t *testing.T) {
	exp := exporter.NewEprExporter(&eprExportProductRepo{}, &eprExportVariantRepo{}, nil)
	h := admin.NewEprReportHandler(exp)
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/reports/epr", shophttp.RequirePermission(rbac.ProductsRead)(h.Export()))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reports/epr", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want unauthorized/forbidden", rec.Code)
	}
}
