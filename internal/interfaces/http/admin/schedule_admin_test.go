package admin_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	schedulerApp "github.com/akarso/shopanda/internal/application/scheduler"
	domainadmin "github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
	domainscheduler "github.com/akarso/shopanda/internal/domain/scheduler"
	"github.com/akarso/shopanda/internal/interfaces/http/admin"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// spyAuditLogRepo records every inserted audit entry, for asserting that a
// handler path actually audits (as opposed to just returning the right
// HTTP status).
type spyAuditLogRepo struct {
	inserted []domainadmin.AuditLogRecord
}

func (s *spyAuditLogRepo) Insert(_ context.Context, record domainadmin.AuditLogRecord) error {
	s.inserted = append(s.inserted, record)
	return nil
}

func (s *spyAuditLogRepo) List(context.Context, domainadmin.AuditLogFilter) ([]domainadmin.AuditLogRecord, error) {
	return nil, nil
}

func (s *spyAuditLogRepo) DeleteBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}

type fakeScheduleCatalog struct {
	listResult []domainscheduler.CatalogEntry
	listErr    error

	triggerErr error

	setEnabledErr error
}

func (f *fakeScheduleCatalog) List(context.Context) ([]domainscheduler.CatalogEntry, error) {
	return f.listResult, f.listErr
}

func (f *fakeScheduleCatalog) Trigger(context.Context, string) error {
	return f.triggerErr
}

func (f *fakeScheduleCatalog) SetEnabled(context.Context, string, bool) error {
	return f.setEnabledErr
}

// newScheduleAdminRouter mirrors the real wiring in cmd/api/wire_routes.go:
// list behind jobs.read, trigger/enable/disable behind jobs.write — the
// same jobs.read/jobs.write permissions PR-1029 gates job admin with,
// since schedules and jobs are the same conceptual operational surface.
func newScheduleAdminRouter(h *admin.ScheduleAdminHandler) *http.ServeMux {
	requireJobsRead := admin.RequirePermission(rbac.JobsRead)
	requireJobsWrite := admin.RequirePermission(rbac.JobsWrite)
	withAdminContext := admin.AdminContextMiddleware()
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/schedules", withAdminContext(requireJobsRead(h.List())))
	mux.Handle("POST /api/v1/admin/schedules/{name}/trigger", withAdminContext(requireJobsWrite(h.Trigger())))
	mux.Handle("POST /api/v1/admin/schedules/{name}/enable", withAdminContext(requireJobsWrite(h.Enable())))
	mux.Handle("POST /api/v1/admin/schedules/{name}/disable", withAdminContext(requireJobsWrite(h.Disable())))
	return mux
}

func newScheduleAdminHandler(t *testing.T, catalog domainscheduler.Catalog) *admin.ScheduleAdminHandler {
	t.Helper()
	svc, err := schedulerApp.NewService(catalog)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return admin.NewScheduleAdminHandler(svc, adminapp.NewAuditor(logger.New("error")))
}

func TestScheduleAdminHandler_List(t *testing.T) {
	now := time.Now()
	catalog := &fakeScheduleCatalog{
		listResult: []domainscheduler.CatalogEntry{
			{Name: "cache.cleanup", Spec: "*/5 * * * *", NextRun: now, Enabled: true},
		},
	}
	h := newScheduleAdminHandler(t, catalog)
	mux := newScheduleAdminRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/schedules", nil)
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Schedules []map[string]interface{} `json:"schedules"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data.Schedules) != 1 || resp.Data.Schedules[0]["name"] != "cache.cleanup" {
		t.Fatalf("schedules = %+v, want one cache.cleanup entry", resp.Data.Schedules)
	}
	if resp.Data.Schedules[0]["enabled"] != true {
		t.Errorf("enabled = %v, want true", resp.Data.Schedules[0]["enabled"])
	}
}

func TestScheduleAdminHandler_List_Forbidden(t *testing.T) {
	h := newScheduleAdminHandler(t, &fakeScheduleCatalog{})
	mux := newScheduleAdminRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/schedules", nil)
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestScheduleAdminHandler_List_CatalogError(t *testing.T) {
	h := newScheduleAdminHandler(t, &fakeScheduleCatalog{listErr: errors.New("db down")})
	mux := newScheduleAdminRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/schedules", nil)
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}

func TestScheduleAdminHandler_Trigger_Success(t *testing.T) {
	h := newScheduleAdminHandler(t, &fakeScheduleCatalog{})
	mux := newScheduleAdminRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/schedules/cache.cleanup/trigger", nil)
	req.SetPathValue("name", "cache.cleanup")
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestScheduleAdminHandler_Trigger_NotFound(t *testing.T) {
	h := newScheduleAdminHandler(t, &fakeScheduleCatalog{triggerErr: apperror.NotFound("no scheduled task named \"missing\"")})
	mux := newScheduleAdminRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/schedules/missing/trigger", nil)
	req.SetPathValue("name", "missing")
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// TestScheduleAdminHandler_Trigger_ConflictNoLocalScheduler pins the
// "this process has no embedded scheduler" contract (PR-1030's
// cross-process design) surfaces as a 409, not a 500 or a silent success.
func TestScheduleAdminHandler_Trigger_ConflictNoLocalScheduler(t *testing.T) {
	h := newScheduleAdminHandler(t, &fakeScheduleCatalog{triggerErr: apperror.Conflict("this server process has no embedded scheduler to trigger from")})
	mux := newScheduleAdminRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/schedules/cache.cleanup/trigger", nil)
	req.SetPathValue("name", "cache.cleanup")
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestScheduleAdminHandler_Trigger_Forbidden(t *testing.T) {
	h := newScheduleAdminHandler(t, &fakeScheduleCatalog{})
	mux := newScheduleAdminRouter(h)

	// Support has neither jobs.read nor jobs.write (admin-only, like audit.read).
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/schedules/cache.cleanup/trigger", nil)
	req.SetPathValue("name", "cache.cleanup")
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

// TestScheduleAdminHandler_Trigger_EmptyName calls the handler func
// directly (not through the mux) since net/http's {name} pattern never
// actually routes an empty path segment here — this exercises the
// handler's own defense-in-depth check, matching job_admin_test.go's
// Retry_EmptyID test.
func TestScheduleAdminHandler_Trigger_EmptyName(t *testing.T) {
	h := newScheduleAdminHandler(t, &fakeScheduleCatalog{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/schedules//trigger", nil)
	req.SetPathValue("name", "")
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	h.Trigger()(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

// TestScheduleAdminHandler_Trigger_EmptyName_IsAudited pins the fix for a
// validation failure silently bypassing the audit trail: an admin's
// attempted (even if malformed) trigger request must still leave a record,
// same as any other error path in this handler.
func TestScheduleAdminHandler_Trigger_EmptyName_IsAudited(t *testing.T) {
	svc, err := schedulerApp.NewService(&fakeScheduleCatalog{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	repo := &spyAuditLogRepo{}
	auditor := adminapp.NewAuditor(logger.New("error"))
	auditor.SetAuditLogRepository(repo)
	h := admin.NewScheduleAdminHandler(svc, auditor)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/schedules//trigger", nil)
	req.SetPathValue("name", "")
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	h.Trigger()(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if len(repo.inserted) != 1 {
		t.Fatalf("audit inserts = %d, want 1", len(repo.inserted))
	}
	if repo.inserted[0].Action != string(adminapp.AuditScheduleTrigger) {
		t.Errorf("Action = %q, want %q", repo.inserted[0].Action, adminapp.AuditScheduleTrigger)
	}
	if repo.inserted[0].Result != "error" {
		t.Errorf("Result = %q, want %q", repo.inserted[0].Result, "error")
	}
}

// TestScheduleAdminHandler_Enable_EmptyName_IsAudited is the same pin for
// the enable/disable path, which shares the setEnabled helper.
func TestScheduleAdminHandler_Enable_EmptyName_IsAudited(t *testing.T) {
	svc, err := schedulerApp.NewService(&fakeScheduleCatalog{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	repo := &spyAuditLogRepo{}
	auditor := adminapp.NewAuditor(logger.New("error"))
	auditor.SetAuditLogRepository(repo)
	h := admin.NewScheduleAdminHandler(svc, auditor)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/schedules//enable", nil)
	req.SetPathValue("name", "")
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	h.Enable()(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if len(repo.inserted) != 1 {
		t.Fatalf("audit inserts = %d, want 1", len(repo.inserted))
	}
	if repo.inserted[0].Action != string(adminapp.AuditScheduleEnable) {
		t.Errorf("Action = %q, want %q", repo.inserted[0].Action, adminapp.AuditScheduleEnable)
	}
	if repo.inserted[0].Result != "error" {
		t.Errorf("Result = %q, want %q", repo.inserted[0].Result, "error")
	}
}

func TestScheduleAdminHandler_Enable_Success(t *testing.T) {
	h := newScheduleAdminHandler(t, &fakeScheduleCatalog{})
	mux := newScheduleAdminRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/schedules/cache.cleanup/enable", nil)
	req.SetPathValue("name", "cache.cleanup")
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data["enabled"] != true {
		t.Errorf("enabled = %v, want true", resp.Data["enabled"])
	}
}

func TestScheduleAdminHandler_Disable_Success(t *testing.T) {
	h := newScheduleAdminHandler(t, &fakeScheduleCatalog{})
	mux := newScheduleAdminRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/schedules/cache.cleanup/disable", nil)
	req.SetPathValue("name", "cache.cleanup")
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data["enabled"] != false {
		t.Errorf("enabled = %v, want false", resp.Data["enabled"])
	}
}

func TestScheduleAdminHandler_Disable_NotFound(t *testing.T) {
	h := newScheduleAdminHandler(t, &fakeScheduleCatalog{setEnabledErr: apperror.NotFound("no scheduled task named \"missing\"")})
	mux := newScheduleAdminRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/schedules/missing/disable", nil)
	req.SetPathValue("name", "missing")
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestScheduleAdminHandler_Enable_Forbidden(t *testing.T) {
	h := newScheduleAdminHandler(t, &fakeScheduleCatalog{})
	mux := newScheduleAdminRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/schedules/cache.cleanup/enable", nil)
	req.SetPathValue("name", "cache.cleanup")
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}
