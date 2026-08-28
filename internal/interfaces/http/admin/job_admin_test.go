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
	jobsApp "github.com/akarso/shopanda/internal/application/jobs"
	"github.com/akarso/shopanda/internal/domain/identity"
	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/interfaces/http/admin"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type fakeJobReader struct {
	listResult []domainjobs.Summary
	listErr    error

	getResult *domainjobs.Detail
	getErr    error

	countsResult map[domainjobs.Status]int
	countsErr    error
}

func (f *fakeJobReader) List(context.Context, domainjobs.ListFilter) ([]domainjobs.Summary, error) {
	return f.listResult, f.listErr
}

func (f *fakeJobReader) Get(_ context.Context, id string) (*domainjobs.Detail, error) {
	return f.getResult, f.getErr
}

func (f *fakeJobReader) CountsByStatus(context.Context) (map[domainjobs.Status]int, error) {
	return f.countsResult, f.countsErr
}

type fakeJobAdmin struct {
	retryErr  error
	cancelErr error
}

func (f *fakeJobAdmin) Retry(context.Context, string) error {
	return f.retryErr
}

func (f *fakeJobAdmin) Cancel(context.Context, string) error {
	return f.cancelErr
}

// newJobAdminRouter mirrors the real wiring in cmd/api/wire_routes.go:
// list/detail behind jobs.read, retry/cancel behind jobs.write.
func newJobAdminRouter(h *admin.JobAdminHandler) *http.ServeMux {
	requireJobsRead := admin.RequirePermission(rbac.JobsRead)
	requireJobsWrite := admin.RequirePermission(rbac.JobsWrite)
	withAdminContext := admin.AdminContextMiddleware()
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/jobs", withAdminContext(requireJobsRead(h.List())))
	mux.Handle("GET /api/v1/admin/jobs/{id}", withAdminContext(requireJobsRead(h.Get())))
	mux.Handle("POST /api/v1/admin/jobs/{id}/retry", withAdminContext(requireJobsWrite(h.Retry())))
	mux.Handle("POST /api/v1/admin/jobs/{id}/cancel", withAdminContext(requireJobsWrite(h.Cancel())))
	return mux
}

func newJobAdminHandler(t *testing.T, reader domainjobs.Reader, adm domainjobs.Admin) *admin.JobAdminHandler {
	t.Helper()
	svc, err := jobsApp.NewService(reader, adm)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return admin.NewJobAdminHandler(svc, adminapp.NewAuditor(logger.New("error")))
}

func TestJobAdminHandler_List(t *testing.T) {
	now := time.Now()
	reader := &fakeJobReader{
		listResult: []domainjobs.Summary{
			{ID: "job-1", Type: "webhook.deliver", Status: domainjobs.StatusFailed, Attempts: 3, MaxRetries: 5, RunAt: now, CreatedAt: now, UpdatedAt: now},
		},
	}
	h := newJobAdminHandler(t, reader, &fakeJobAdmin{})
	mux := newJobAdminRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/jobs", nil)
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Jobs []map[string]interface{} `json:"jobs"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data.Jobs) != 1 || resp.Data.Jobs[0]["id"] != "job-1" {
		t.Fatalf("jobs = %+v, want one job-1 entry", resp.Data.Jobs)
	}
}

func TestJobAdminHandler_List_Forbidden(t *testing.T) {
	h := newJobAdminHandler(t, &fakeJobReader{}, &fakeJobAdmin{})
	mux := newJobAdminRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/jobs", nil)
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

// TestJobAdminHandler_Get_EmptyID pins the validation contract: an empty id
// is rejected as 422 (apperror.Validation), matching how every other
// *_admin.go handler in this codebase maps CodeValidation, not a generic
// 400 or 500. Calls the handler func directly (not through the mux) since
// net/http's {id} pattern never actually dispatches an empty path segment
// here — this exercises the handler's own defense-in-depth check.
func TestJobAdminHandler_Get_EmptyID(t *testing.T) {
	h := newJobAdminHandler(t, &fakeJobReader{}, &fakeJobAdmin{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/jobs/", nil)
	req.SetPathValue("id", "")
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	h.Get()(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestJobAdminHandler_List_ReaderError(t *testing.T) {
	h := newJobAdminHandler(t, &fakeJobReader{listErr: errors.New("db down")}, &fakeJobAdmin{})
	mux := newJobAdminRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/jobs", nil)
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}

func TestJobAdminHandler_Get_ReaderError(t *testing.T) {
	h := newJobAdminHandler(t, &fakeJobReader{getErr: errors.New("db down")}, &fakeJobAdmin{})
	mux := newJobAdminRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/jobs/job-1", nil)
	req.SetPathValue("id", "job-1")
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}

func TestJobAdminHandler_Get_NotFound(t *testing.T) {
	h := newJobAdminHandler(t, &fakeJobReader{getResult: nil}, &fakeJobAdmin{})
	mux := newJobAdminRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/jobs/missing", nil)
	req.SetPathValue("id", "missing")
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestJobAdminHandler_Get_IncludesPayloadAndLastError(t *testing.T) {
	now := time.Now()
	detail := &domainjobs.Detail{
		Summary: domainjobs.Summary{
			ID: "job-1", Type: "webhook.deliver", Status: domainjobs.StatusFailed,
			Attempts: 3, MaxRetries: 5, RunAt: now, CreatedAt: now, UpdatedAt: now,
		},
		Payload:   map[string]interface{}{"endpoint_id": "ep-1"},
		LastError: "connection refused",
	}
	h := newJobAdminHandler(t, &fakeJobReader{getResult: detail}, &fakeJobAdmin{})
	mux := newJobAdminRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/jobs/job-1", nil)
	req.SetPathValue("id", "job-1")
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
	if resp.Data["last_error"] != "connection refused" {
		t.Errorf("last_error = %v, want 'connection refused'", resp.Data["last_error"])
	}
	payload, ok := resp.Data["payload"].(map[string]interface{})
	if !ok || payload["endpoint_id"] != "ep-1" {
		t.Errorf("payload = %+v, want endpoint_id=ep-1", resp.Data["payload"])
	}
}

func TestJobAdminHandler_Retry_Success(t *testing.T) {
	h := newJobAdminHandler(t, &fakeJobReader{}, &fakeJobAdmin{})
	mux := newJobAdminRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/jobs/job-1/retry", nil)
	req.SetPathValue("id", "job-1")
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// TestJobAdminHandler_Retry_ConflictOnNonFailedJob pins the 409 contract:
// retrying a job that isn't currently failed is a conflict, not silently
// ignored or a generic 500.
func TestJobAdminHandler_Retry_ConflictOnNonFailedJob(t *testing.T) {
	h := newJobAdminHandler(t, &fakeJobReader{}, &fakeJobAdmin{retryErr: apperror.Conflict("job is pending, not failed")})
	mux := newJobAdminRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/jobs/job-1/retry", nil)
	req.SetPathValue("id", "job-1")
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestJobAdminHandler_Retry_NotFound(t *testing.T) {
	h := newJobAdminHandler(t, &fakeJobReader{}, &fakeJobAdmin{retryErr: apperror.NotFound("job not found")})
	mux := newJobAdminRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/jobs/missing/retry", nil)
	req.SetPathValue("id", "missing")
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestJobAdminHandler_Retry_Forbidden(t *testing.T) {
	h := newJobAdminHandler(t, &fakeJobReader{}, &fakeJobAdmin{})
	mux := newJobAdminRouter(h)

	// Support has neither jobs.read nor jobs.write (admin-only, like audit.read).
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/jobs/job-1/retry", nil)
	req.SetPathValue("id", "job-1")
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

// TestJobAdminHandler_Retry_EmptyID calls the handler func directly (not
// through the mux) for the same reason as Get_EmptyID above — net/http
// never actually routes an empty {id} segment here.
func TestJobAdminHandler_Retry_EmptyID(t *testing.T) {
	h := newJobAdminHandler(t, &fakeJobReader{}, &fakeJobAdmin{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/jobs//retry", nil)
	req.SetPathValue("id", "")
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	h.Retry()(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestJobAdminHandler_Cancel_Success(t *testing.T) {
	h := newJobAdminHandler(t, &fakeJobReader{}, &fakeJobAdmin{})
	mux := newJobAdminRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/jobs/job-1/cancel", nil)
	req.SetPathValue("id", "job-1")
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// TestJobAdminHandler_Cancel_ConflictOnProcessingJob pins the 409 contract
// for the most likely point of operator confusion: a processing job cannot
// be cancelled (no in-flight cancellation).
func TestJobAdminHandler_Cancel_ConflictOnProcessingJob(t *testing.T) {
	h := newJobAdminHandler(t, &fakeJobReader{}, &fakeJobAdmin{cancelErr: apperror.Conflict("job is currently processing and cannot be cancelled")})
	mux := newJobAdminRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/jobs/job-1/cancel", nil)
	req.SetPathValue("id", "job-1")
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestJobAdminHandler_Cancel_NotFound(t *testing.T) {
	h := newJobAdminHandler(t, &fakeJobReader{}, &fakeJobAdmin{cancelErr: apperror.NotFound("job not found")})
	mux := newJobAdminRouter(h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/jobs/missing/cancel", nil)
	req.SetPathValue("id", "missing")
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestJobAdminHandler_Cancel_Forbidden(t *testing.T) {
	h := newJobAdminHandler(t, &fakeJobReader{}, &fakeJobAdmin{})
	mux := newJobAdminRouter(h)

	// Support has neither jobs.read nor jobs.write (admin-only, like audit.read).
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/jobs/job-1/cancel", nil)
	req.SetPathValue("id", "job-1")
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}
