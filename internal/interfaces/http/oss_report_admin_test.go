package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/exporter"
	"github.com/akarso/shopanda/internal/domain/legal"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/rbac"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
)

type ossReportOrderRepo struct {
	rows []order.TaxSnapshotRow
}

func (m *ossReportOrderRepo) FindByID(context.Context, string) (*order.Order, error) { return nil, nil }
func (m *ossReportOrderRepo) FindByCustomerID(context.Context, string) ([]order.Order, error) {
	return nil, nil
}
func (m *ossReportOrderRepo) FindByContactEmail(context.Context, string) ([]order.Order, error) {
	return nil, nil
}
func (m *ossReportOrderRepo) List(context.Context, int, int) ([]order.Order, error) { return nil, nil }
func (m *ossReportOrderRepo) Save(context.Context, *order.Order) error               { return nil }
func (m *ossReportOrderRepo) UpdateStatus(context.Context, *order.Order) error       { return nil }
func (m *ossReportOrderRepo) LinkToCustomer(context.Context, *order.Order) error     { return nil }
func (m *ossReportOrderRepo) LinkToCustomerByContactEmail(context.Context, string, string, time.Time) (int64, error) {
	return 0, nil
}
func (m *ossReportOrderRepo) ListPaidTaxSnapshots(_ context.Context, _, _ time.Time) ([]order.TaxSnapshotRow, error) {
	return m.rows, nil
}

type stubOssReportConfig map[string]interface{}

func (s stubOssReportConfig) Get(_ context.Context, key string) (interface{}, error) {
	v, ok := s[key]
	if !ok {
		return nil, nil
	}
	return v, nil
}

func TestOssReportHandler_ExportDetailCSV(t *testing.T) {
	repo := &ossReportOrderRepo{
		rows: []order.TaxSnapshotRow{{
			OrderID:            "ord-99",
			CreatedAt:          time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC),
			DestinationCountry: "NL",
			Currency:           "EUR",
			SubtotalAmount:     5000,
			TaxAmount:          1050,
		}},
	}
	cfg := stubOssReportConfig{legal.OssEnabledConfigKey: true}
	exp := exporter.NewOssExporter(repo, cfg)
	h := shophttp.NewOssReportHandler(exp)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reports/oss?from=2026-01-01&to=2026-12-31", nil)
	h.Export()(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "ord-99") || !strings.Contains(body, "NL") {
		t.Fatalf("unexpected csv: %s", body)
	}
}

func TestOssReportHandler_GuestUnauthorized(t *testing.T) {
	exp := exporter.NewOssExporter(&ossReportOrderRepo{}, stubOssReportConfig{legal.OssEnabledConfigKey: true})
	h := shophttp.NewOssReportHandler(exp)
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/reports/oss", shophttp.RequirePermission(rbac.OrdersRead)(h.Export()))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reports/oss", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestOssReportHandler_CustomerForbidden(t *testing.T) {
	exp := exporter.NewOssExporter(&ossReportOrderRepo{}, stubOssReportConfig{legal.OssEnabledConfigKey: true})
	h := shophttp.NewOssReportHandler(exp)
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/reports/oss", shophttp.RequirePermission(rbac.OrdersRead)(h.Export()))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reports/oss", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestOssReportHandler_DisabledForbidden(t *testing.T) {
	exp := exporter.NewOssExporter(&ossReportOrderRepo{}, stubOssReportConfig{legal.OssEnabledConfigKey: false})
	h := shophttp.NewOssReportHandler(exp)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reports/oss?from=2026-01-01&to=2026-12-31", nil)
	h.Export()(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}
