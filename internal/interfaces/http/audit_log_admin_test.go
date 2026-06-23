package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/admin"
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

func TestAuditLogAdmin_List_ReturnsEntries(t *testing.T) {
	created := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	repo := &mockAuditLogListRepo{
		entries: []domainadmin.AuditLogRecord{{
			ID:           "audit-1",
			CreatedAt:    created,
			AdminID:      "admin-1",
			Action:       string(admin.AuditProductUpdate),
			ResourceType: "product",
			ResourceID:   "prod-1",
			Result:       "success",
			StoreID:      "store-eu",
			Language:     "en",
			Currency:     "EUR",
			Metadata:     map[string]interface{}{"name": "Updated"},
		}},
	}
	auditor := admin.NewAuditor(logger.New("error"))
	h := shophttp.NewAuditLogAdminHandler(repo, auditor)

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
			Action:       string(admin.AuditProductUpdate),
			ResourceType: "product",
			ResourceID:   "prod-1",
			Result:       "success",
		}},
	}
	auditor := admin.NewAuditor(logger.New("error"))
	h := shophttp.NewAuditLogAdminHandler(repo, auditor)

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

func TestAuditLogAdmin_List_ForbiddenWithoutPermission(t *testing.T) {
	repo := &mockAuditLogListRepo{}
	auditor := admin.NewAuditor(logger.New("error"))
	h := shophttp.NewAuditLogAdminHandler(repo, auditor)

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
