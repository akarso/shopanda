package admin_test

import (
	"context"
	"encoding/json"
	"github.com/akarso/shopanda/internal/interfaces/http/admin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	domainadmin "github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type mockAuditLogListRepo struct {
	entries []domainadmin.AuditLogRecord
	last    domainadmin.AuditLogFilter
}

func (m *mockAuditLogListRepo) Insert(context.Context, domainadmin.AuditLogRecord) error {
	return nil
}

func (m *mockAuditLogListRepo) List(_ context.Context, filter domainadmin.AuditLogFilter) ([]domainadmin.AuditLogRecord, error) {
	m.last = filter
	return m.entries, nil
}

func (m *mockAuditLogListRepo) DeleteBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func TestAuditLogAdmin_List_ReturnsEntries(t *testing.T) {
	created := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	repo := &mockAuditLogListRepo{
		entries: []domainadmin.AuditLogRecord{{
			ID:           "audit-1",
			CreatedAt:    created,
			AdminID:      "admin-1",
			Action:       string(adminapp.AuditProductUpdate),
			ResourceType: "product",
			ResourceID:   "prod-1",
			Result:       "success",
			StoreID:      "store-eu",
			Language:     "en",
			Currency:     "EUR",
			Metadata:     map[string]interface{}{"name": "Updated"},
		}},
	}
	auditor := adminapp.NewAuditor(logger.New("error"))
	h := admin.NewAuditLogAdminHandler(repo, auditor)

	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/audit", shophttp.RequirePermission(rbac.AuditRead)(h.List()))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit?offset=0&limit=20&action=product.update", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if repo.last.Action != "product.update" {
		t.Fatalf("filter action = %q, want product.update", repo.last.Action)
	}

	var envelope struct {
		Data struct {
			Entries []map[string]interface{} `json:"entries"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(envelope.Data.Entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(envelope.Data.Entries))
	}
	if envelope.Data.Entries[0]["store_id"] != "store-eu" {
		t.Fatalf("store_id = %v", envelope.Data.Entries[0]["store_id"])
	}
}

func TestAuditLogAdmin_List_DateOnlyToIncludesSameDayEntries(t *testing.T) {
	afternoon := time.Date(2026, 6, 1, 15, 30, 0, 0, time.UTC)
	repo := &filteringAuditLogListRepo{
		entries: []domainadmin.AuditLogRecord{{
			ID:           "audit-afternoon",
			CreatedAt:    afternoon,
			AdminID:      "admin-1",
			Action:       string(adminapp.AuditProductUpdate),
			ResourceType: "product",
			ResourceID:   "prod-1",
			Result:       "success",
		}},
	}
	auditor := adminapp.NewAuditor(logger.New("error"))
	h := admin.NewAuditLogAdminHandler(repo, auditor)

	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/audit", shophttp.RequirePermission(rbac.AuditRead)(h.List()))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit?to=2026-06-01", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	wantTo := time.Date(2026, 6, 1, 23, 59, 59, 999999999, time.UTC)
	if repo.last.To == nil {
		t.Fatal("expected to filter to be set")
	}
	if !repo.last.To.Equal(wantTo) {
		t.Fatalf("to filter = %v, want %v", repo.last.To, wantTo)
	}

	var envelope struct {
		Data struct {
			Entries []map[string]interface{} `json:"entries"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(envelope.Data.Entries) != 1 {
		t.Fatalf("entries len = %d, want 1 (same-day entry should be included)", len(envelope.Data.Entries))
	}
}

type filteringAuditLogListRepo struct {
	entries []domainadmin.AuditLogRecord
	last    domainadmin.AuditLogFilter
}

func (m *filteringAuditLogListRepo) Insert(context.Context, domainadmin.AuditLogRecord) error {
	return nil
}

func (m *filteringAuditLogListRepo) List(_ context.Context, filter domainadmin.AuditLogFilter) ([]domainadmin.AuditLogRecord, error) {
	m.last = filter
	out := make([]domainadmin.AuditLogRecord, 0, len(m.entries))
	for _, entry := range m.entries {
		if filter.From != nil && entry.CreatedAt.Before(*filter.From) {
			continue
		}
		if filter.To != nil && entry.CreatedAt.After(*filter.To) {
			continue
		}
		out = append(out, entry)
	}
	return out, nil
}

func (m *filteringAuditLogListRepo) DeleteBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func TestAuditLogAdmin_List_ForbiddenWithoutPermission(t *testing.T) {
	repo := &mockAuditLogListRepo{}
	auditor := adminapp.NewAuditor(logger.New("error"))
	h := admin.NewAuditLogAdminHandler(repo, auditor)

	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/audit", shophttp.RequirePermission(rbac.AuditRead)(h.List()))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit", nil)
	req = testhelper.AuthenticatedRequest(req, "editor-1", identity.RoleEditor)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestAuditLogAdmin_Export_CSV(t *testing.T) {
	created := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	repo := &mockAuditLogListRepo{
		entries: []domainadmin.AuditLogRecord{{
			ID:           "audit-1",
			CreatedAt:    created,
			AdminID:      "admin-1",
			Action:       "product.update",
			ResourceType: "product",
			ResourceID:   "prod-1",
			Result:       "success",
		}},
	}
	auditor := adminapp.NewAuditor(logger.New("error"))
	h := admin.NewAuditLogAdminHandler(repo, auditor)

	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/audit/export", shophttp.RequirePermission(rbac.AuditRead)(h.Export()))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit/export?format=csv", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Fatalf("content-type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "audit-1") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestAuditLogAdmin_Export_JSON(t *testing.T) {
	repo := &mockAuditLogListRepo{
		entries: []domainadmin.AuditLogRecord{{
			ID:        "audit-1",
			CreatedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
			AdminID:   "admin-1",
			Action:    "audit.list",
			Result:    "success",
		}},
	}
	h := admin.NewAuditLogAdminHandler(repo, adminapp.NewAuditor(logger.New("error")))

	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/audit/export", shophttp.RequirePermission(rbac.AuditRead)(h.Export()))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit/export?format=json", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var items []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(items) != 1 || items[0]["id"] != "audit-1" {
		t.Fatalf("items = %#v", items)
	}
}

func TestAuditLogAdmin_Export_ForwardsFilters(t *testing.T) {
	repo := &mockAuditLogListRepo{
		entries: []domainadmin.AuditLogRecord{{
			ID:           "audit-1",
			CreatedAt:    time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
			AdminID:      "admin-1",
			Action:       "product.update",
			ResourceType: "product",
			ResourceID:   "prod-1",
			Result:       "success",
		}},
	}
	h := admin.NewAuditLogAdminHandler(repo, adminapp.NewAuditor(logger.New("error")))

	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/audit/export", shophttp.RequirePermission(rbac.AuditRead)(h.Export()))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/admin/audit/export?format=csv&action=product.update&resource_type=product&resource_id=prod-1&from=2026-01-01&to=2026-06-01",
		nil,
	)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if repo.last.Action != "product.update" {
		t.Fatalf("action filter = %q, want product.update", repo.last.Action)
	}
	if repo.last.ResourceType != "product" {
		t.Fatalf("resource_type filter = %q, want product", repo.last.ResourceType)
	}
	if repo.last.ResourceID != "prod-1" {
		t.Fatalf("resource_id filter = %q, want prod-1", repo.last.ResourceID)
	}
	wantFrom := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if repo.last.From == nil || !repo.last.From.Equal(wantFrom) {
		t.Fatalf("from filter = %v, want %v", repo.last.From, wantFrom)
	}
	wantTo := time.Date(2026, 6, 1, 23, 59, 59, 999999999, time.UTC)
	if repo.last.To == nil || !repo.last.To.Equal(wantTo) {
		t.Fatalf("to filter = %v, want %v", repo.last.To, wantTo)
	}
}
