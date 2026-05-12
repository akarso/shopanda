package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/domain/rbac"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type mockStatsRepo struct {
	getDashboardStatsFn func(ctx context.Context, lowStockThreshold, recentLimit int) (admin.DashboardStats, error)
}

func (m *mockStatsRepo) GetDashboardStats(ctx context.Context, lowStockThreshold, recentLimit int) (admin.DashboardStats, error) {
	if m.getDashboardStatsFn != nil {
		return m.getDashboardStatsFn(ctx, lowStockThreshold, recentLimit)
	}
	return admin.DashboardStats{}, nil
}

func newStatsAdminRouterWithAudit(h *shophttp.StatsAdminHandler) *http.ServeMux {
	requireOrdersRead := shophttp.RequirePermission(rbac.OrdersRead)
	withAdminContext := shophttp.AdminContextMiddleware()
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/stats/overview", withAdminContext(requireOrdersRead(h.Overview())))
	return mux
}

func TestStatsAdminHandler_Overview_AuditIncludesScopeContext(t *testing.T) {
	repo := &mockStatsRepo{
		getDashboardStatsFn: func(_ context.Context, lowStockThreshold, recentLimit int) (admin.DashboardStats, error) {
			if lowStockThreshold != 10 {
				t.Errorf("lowStockThreshold = %d, want 10", lowStockThreshold)
			}
			if recentLimit != 10 {
				t.Errorf("recentLimit = %d, want 10", recentLimit)
			}
			return admin.DashboardStats{
				OrdersToday:   3,
				RevenueToday:  4200,
				Currency:      "EUR",
				TotalProducts: 15,
				LowStockCount: 2,
				RecentOrders: []admin.RecentOrder{{
					ID:          "ord-1",
					CustomerID:  "cust-1",
					TotalAmount: 4200,
					Currency:    "EUR",
					Status:      "paid",
					CreatedAt:   "2026-05-12T10:00:00Z",
				}},
			}, nil
		},
	}
	sink := &auditSink{}
	h := shophttp.NewStatsAdminHandlerWithAuditor(repo, adminapp.NewAuditor(sink))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/stats/overview", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	req.Header.Set("X-Admin-Currency", "EUR")
	newStatsAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
	}
	if got := entry.context["action"]; got != adminapp.AuditStatsRead {
		t.Errorf("action = %v, want %q", got, adminapp.AuditStatsRead)
	}
	if got := entry.context["resource_type"]; got != "stats_overview" {
		t.Errorf("resource_type = %v, want %q", got, "stats_overview")
	}
	if got := entry.context["detail_store_id"]; got != "store-eu" {
		t.Errorf("detail_store_id = %v, want %q", got, "store-eu")
	}
	if got := entry.context["detail_language"]; got != "en" {
		t.Errorf("detail_language = %v, want %q", got, "en")
	}
	if got := entry.context["detail_currency"]; got != "EUR" {
		t.Errorf("detail_currency = %v, want %q", got, "EUR")
	}
	if got := entry.context["detail_low_stock_threshold"]; got != 10 {
		t.Errorf("detail_low_stock_threshold = %v, want %d", got, 10)
	}
	if got := entry.context["detail_recent_limit"]; got != 10 {
		t.Errorf("detail_recent_limit = %v, want %d", got, 10)
	}
	if got := entry.context["admin_id"]; got != "admin-1" {
		t.Errorf("admin_id = %v, want %q", got, "admin-1")
	}
	if got := entry.context["result"]; got != "success" {
		t.Errorf("result = %v, want %q", got, "success")
	}
}

func TestStatsAdminHandler_Overview_AuditFailureIncludesError(t *testing.T) {
	repo := &mockStatsRepo{
		getDashboardStatsFn: func(_ context.Context, lowStockThreshold, recentLimit int) (admin.DashboardStats, error) {
			return admin.DashboardStats{}, errors.New("stats unavailable")
		},
	}
	sink := &auditSink{}
	h := shophttp.NewStatsAdminHandlerWithAuditor(repo, adminapp.NewAuditor(sink))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/stats/overview", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	req.Header.Set("X-Admin-Currency", "EUR")
	newStatsAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action.failed" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action.failed")
	}
	if got := entry.context["action"]; got != adminapp.AuditStatsRead {
		t.Errorf("action = %v, want %q", got, adminapp.AuditStatsRead)
	}
	if got := entry.context["resource_type"]; got != "stats_overview" {
		t.Errorf("resource_type = %v, want %q", got, "stats_overview")
	}
	if got := entry.context["result"]; got != "error" {
		t.Errorf("result = %v, want %q", got, "error")
	}
	if got := entry.context["error"]; got == nil || got == "" {
		t.Errorf("error = %v, want non-empty", got)
	}
	if got := entry.context["detail_store_id"]; got != "store-eu" {
		t.Errorf("detail_store_id = %v, want %q", got, "store-eu")
	}
	if got := entry.context["detail_language"]; got != "en" {
		t.Errorf("detail_language = %v, want %q", got, "en")
	}
	if got := entry.context["detail_currency"]; got != "EUR" {
		t.Errorf("detail_currency = %v, want %q", got, "EUR")
	}
	if got := entry.context["detail_low_stock_threshold"]; got != 10 {
		t.Errorf("detail_low_stock_threshold = %v, want %d", got, 10)
	}
	if got := entry.context["detail_recent_limit"]; got != 10 {
		t.Errorf("detail_recent_limit = %v, want %d", got, 10)
	}
}

func TestStatsAdminHandler_Overview_CustomerForbidden(t *testing.T) {
	repo := &mockStatsRepo{}
	h := shophttp.NewStatsAdminHandlerWithAuditor(repo, adminapp.NewAuditor(logger.NewWithWriter(io.Discard, "info")))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/stats/overview", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	newStatsAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestStatsAdminHandler_Overview_GuestUnauthorized(t *testing.T) {
	repo := &mockStatsRepo{}
	h := shophttp.NewStatsAdminHandlerWithAuditor(repo, adminapp.NewAuditor(logger.NewWithWriter(io.Discard, "info")))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/stats/overview", nil)
	newStatsAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}
