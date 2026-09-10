package admin_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	searchApp "github.com/akarso/shopanda/internal/application/search"
	domainadmin "github.com/akarso/shopanda/internal/domain/admin"
	"github.com/akarso/shopanda/internal/domain/identity"
	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/rbac"
	domainsearch "github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/interfaces/http/admin"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type fakeSearchRunStore struct {
	runs map[string]*domainsearch.Run
}

func (f *fakeSearchRunStore) Create(_ context.Context, run domainsearch.Run) error {
	if f.runs == nil {
		f.runs = map[string]*domainsearch.Run{}
	}
	r := run
	f.runs[run.ID] = &r
	return nil
}

func (f *fakeSearchRunStore) Get(_ context.Context, id string) (*domainsearch.Run, error) {
	return f.runs[id], nil
}

func (f *fakeSearchRunStore) UpdateProgress(context.Context, string, int, int, int) error { return nil }

func (f *fakeSearchRunStore) Finish(_ context.Context, id string, status domainsearch.RunStatus, lastErr string) error {
	r := f.runs[id]
	if r == nil {
		return errors.New("no such run")
	}
	r.Status = status
	r.LastError = lastErr
	return nil
}

func (f *fakeSearchRunStore) FindStaleProcessing(context.Context, time.Time, int) ([]domainsearch.Run, error) {
	return nil, nil
}

type fakeSearchProductSource struct {
	products     map[string]domainsearch.Product
	listByIDsErr error
	countAll     int
	lastCtx      context.Context
}

func (f *fakeSearchProductSource) CountAll(context.Context) (int, error) { return f.countAll, nil }

func (f *fakeSearchProductSource) ListAll(context.Context, int, int) ([]domainsearch.Product, error) {
	return nil, nil
}

func (f *fakeSearchProductSource) ListByIDs(ctx context.Context, ids []string) ([]domainsearch.Product, error) {
	f.lastCtx = ctx
	if f.listByIDsErr != nil {
		return nil, f.listByIDsErr
	}
	var out []domainsearch.Product
	for _, id := range ids {
		if p, ok := f.products[id]; ok {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeSearchProductSource) ProductIDsByCategory(_ context.Context, categoryIDs []string) ([]string, error) {
	return []string{"resolved-product-1"}, nil
}

func (f *fakeSearchProductSource) ProductIDsUpdatedSince(context.Context, time.Time) ([]string, error) {
	return []string{"resolved-product-1"}, nil
}

type fakeSearchEngine struct {
	indexed  []domainsearch.Product
	indexErr error
	lastCtx  context.Context
}

func (f *fakeSearchEngine) Name() string { return "fake" }

func (f *fakeSearchEngine) IndexProduct(ctx context.Context, p domainsearch.Product) error {
	f.lastCtx = ctx
	if f.indexErr != nil {
		return f.indexErr
	}
	f.indexed = append(f.indexed, p)
	return nil
}

func (f *fakeSearchEngine) RemoveProduct(context.Context, string) error { return nil }

func (f *fakeSearchEngine) Search(context.Context, domainsearch.SearchQuery) (domainsearch.SearchResult, error) {
	return domainsearch.SearchResult{}, nil
}

func (f *fakeSearchEngine) Suggest(context.Context, string, int) ([]domainsearch.Suggestion, error) {
	return nil, nil
}

type fakeSearchQueue struct {
	enqueued []domainjobs.Job
}

func (f *fakeSearchQueue) Enqueue(_ context.Context, job domainjobs.Job) error {
	f.enqueued = append(f.enqueued, job)
	return nil
}
func (f *fakeSearchQueue) Dequeue(context.Context) (*domainjobs.Job, error) { return nil, nil }
func (f *fakeSearchQueue) Complete(context.Context, string) error           { return nil }
func (f *fakeSearchQueue) Fail(context.Context, string, error) error        { return nil }

type searchAdminDeps struct {
	runs     *fakeSearchRunStore
	products *fakeSearchProductSource
	engine   *fakeSearchEngine
	queue    *fakeSearchQueue
}

func newSearchAdminHandler(t *testing.T, deps searchAdminDeps) *admin.SearchAdminHandler {
	t.Helper()
	svc, err := searchApp.NewReindexService(deps.runs, deps.products, deps.queue, logger.New("error"), 1.0)
	if err != nil {
		t.Fatalf("NewReindexService: %v", err)
	}
	return admin.NewSearchAdminHandler(svc, deps.runs, deps.products, deps.engine, adminapp.NewAuditor(logger.New("error")))
}

// fakeAuditLogRepository captures persisted audit records so a test can
// assert on what Trigger actually audited, including for a rejected
// request that never reaches triggerBulk/triggerSingleProduct.
type fakeAuditLogRepository struct {
	records []domainadmin.AuditLogRecord
	lastCtx context.Context
}

func (f *fakeAuditLogRepository) Insert(ctx context.Context, record domainadmin.AuditLogRecord) error {
	f.lastCtx = ctx
	f.records = append(f.records, record)
	return nil
}
func (f *fakeAuditLogRepository) List(context.Context, domainadmin.AuditLogFilter) ([]domainadmin.AuditLogRecord, error) {
	return nil, nil
}
func (f *fakeAuditLogRepository) DeleteBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func newSearchAdminHandlerWithAuditRepo(t *testing.T, deps searchAdminDeps) (*admin.SearchAdminHandler, *fakeAuditLogRepository) {
	t.Helper()
	svc, err := searchApp.NewReindexService(deps.runs, deps.products, deps.queue, logger.New("error"), 1.0)
	if err != nil {
		t.Fatalf("NewReindexService: %v", err)
	}
	auditor := adminapp.NewAuditor(logger.New("error"))
	repo := &fakeAuditLogRepository{}
	auditor.SetAuditLogRepository(repo)
	return admin.NewSearchAdminHandler(svc, deps.runs, deps.products, deps.engine, auditor), repo
}

// newSearchAdminRouter mirrors cmd/api/wire_routes.go's wiring: both
// routes gated on search.reindex.
func newSearchAdminRouter(h *admin.SearchAdminHandler) *http.ServeMux {
	requireSearchReindex := admin.RequirePermission(rbac.SearchReindex)
	withAdminContext := admin.AdminContextMiddleware()
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/admin/search/reindex", withAdminContext(requireSearchReindex(h.Trigger())))
	mux.Handle("GET /api/v1/admin/search/reindex/{runID}", withAdminContext(requireSearchReindex(h.Get())))
	return mux
}

func newDefaultDeps() searchAdminDeps {
	return searchAdminDeps{
		runs:     &fakeSearchRunStore{},
		products: &fakeSearchProductSource{products: map[string]domainsearch.Product{}},
		engine:   &fakeSearchEngine{},
		queue:    &fakeSearchQueue{},
	}
}

func triggerRequest(t *testing.T, mux *http.ServeMux, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/search/reindex", strings.NewReader(body))
	req = testhelper.AdminRequest(req, "admin-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestSearchAdminHandler_Trigger_ScopeAll_Enqueues202(t *testing.T) {
	deps := newDefaultDeps()
	mux := newSearchAdminRouter(newSearchAdminHandler(t, deps))

	rec := triggerRequest(t, mux, `{"scope":"all"}`)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if len(deps.queue.enqueued) != 1 {
		t.Fatalf("enqueued %d jobs, want 1", len(deps.queue.enqueued))
	}
	var resp struct {
		Data struct {
			RunID string `json:"run_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.RunID == "" {
		t.Fatal("expected a non-empty run_id")
	}
}

func TestSearchAdminHandler_Trigger_ScopeProducts_MultipleIDs_Enqueues202(t *testing.T) {
	deps := newDefaultDeps()
	mux := newSearchAdminRouter(newSearchAdminHandler(t, deps))

	rec := triggerRequest(t, mux, `{"scope":"products","ids":["`+id.New()+`","`+id.New()+`"]}`)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if len(deps.queue.enqueued) != 1 {
		t.Fatalf("enqueued %d jobs, want 1", len(deps.queue.enqueued))
	}
	if len(deps.engine.indexed) != 0 {
		t.Errorf("engine.indexed = %d, want 0 (multi-ID must not take the synchronous path)", len(deps.engine.indexed))
	}
}

// TestSearchAdminHandler_Trigger_ScopeProducts_SingleID_IndexesSynchronously
// pins PR-1035's core UX distinction: exactly one product ID skips the
// queue and returns 200 with the indexed result inline, not a run ID.
func TestSearchAdminHandler_Trigger_ScopeProducts_SingleID_IndexesSynchronously(t *testing.T) {
	deps := newDefaultDeps()
	productID := id.New()
	deps.products.products[productID] = domainsearch.Product{ID: productID, Name: "Widget"}
	mux := newSearchAdminRouter(newSearchAdminHandler(t, deps))

	rec := triggerRequest(t, mux, `{"scope":"products","ids":["`+productID+`"]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if len(deps.queue.enqueued) != 0 {
		t.Errorf("enqueued %d jobs, want 0 (single-ID product path must be synchronous)", len(deps.queue.enqueued))
	}
	if len(deps.engine.indexed) != 1 || deps.engine.indexed[0].ID != productID {
		t.Fatalf("engine.indexed = %+v, want exactly [%s]", deps.engine.indexed, productID)
	}
	var resp struct {
		Data struct {
			Status    string `json:"status"`
			ProductID string `json:"product_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Status != "indexed" || resp.Data.ProductID != productID {
		t.Errorf("data = %+v, want status=indexed product_id=%s", resp.Data, productID)
	}
}

// TestSearchAdminHandler_Trigger_ScopeProducts_SingleID_BoundedContext pins
// the fix for a request that would otherwise stay blocked (holding a DB
// connection) for as long as a slow ListByIDs/IndexProduct call takes:
// r.Context() alone has no deadline of its own, so triggerSingleProduct
// must derive a bounded one rather than passing r.Context() straight
// through to either call.
func TestSearchAdminHandler_Trigger_ScopeProducts_SingleID_BoundedContext(t *testing.T) {
	deps := newDefaultDeps()
	productID := id.New()
	deps.products.products[productID] = domainsearch.Product{ID: productID, Name: "Widget"}
	mux := newSearchAdminRouter(newSearchAdminHandler(t, deps))

	rec := triggerRequest(t, mux, `{"scope":"products","ids":["`+productID+`"]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if deps.products.lastCtx == nil {
		t.Fatal("ListByIDs was not called")
	}
	if _, ok := deps.products.lastCtx.Deadline(); !ok {
		t.Error("ListByIDs received a context with no deadline")
	}
	if deps.engine.lastCtx == nil {
		t.Fatal("IndexProduct was not called")
	}
	if _, ok := deps.engine.lastCtx.Deadline(); !ok {
		t.Error("IndexProduct received a context with no deadline")
	}
}

// TestSearchAdminHandler_Trigger_ScopeProducts_SingleID_AuditUsesBoundedContext
// pins a fixed bug: audit's own synchronous persistence call
// (Auditor.LogAction -> persistEntry -> AuditLogRepository.Insert) used
// r.Context() unconditionally, even from triggerSingleProduct — so a slow
// audit-log insert could hold the "supposedly bounded" single-product
// request open past singleProductIndexTimeout, past the point where
// ListByIDs/IndexProduct's own bounded context had already protected the
// rest of the path. The repository's Insert must now receive a context
// with a deadline, the same bounded one those two calls get.
func TestSearchAdminHandler_Trigger_ScopeProducts_SingleID_AuditUsesBoundedContext(t *testing.T) {
	deps := newDefaultDeps()
	productID := id.New()
	deps.products.products[productID] = domainsearch.Product{ID: productID, Name: "Widget"}
	h, repo := newSearchAdminHandlerWithAuditRepo(t, deps)
	mux := newSearchAdminRouter(h)

	rec := triggerRequest(t, mux, `{"scope":"products","ids":["`+productID+`"]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if repo.lastCtx == nil {
		t.Fatal("audit repository Insert was not called")
	}
	if _, ok := repo.lastCtx.Deadline(); !ok {
		t.Error("audit's Insert received a context with no deadline — a slow audit write could still block this 'bounded' request indefinitely")
	}
}

// TestSearchAdminHandler_Trigger_TrailingJSONValue_Rejected pins a fixed
// bug: json.Decoder.Decode only reads a single JSON value and silently
// leaves the rest of the body unread, so a request body carrying a valid
// object followed by a second value used to decode the first object
// successfully and go on to trigger a real reindex — silently discarding,
// rather than rejecting, the malformed remainder.
func TestSearchAdminHandler_Trigger_TrailingJSONValue_Rejected(t *testing.T) {
	deps := newDefaultDeps()
	mux := newSearchAdminRouter(newSearchAdminHandler(t, deps))

	rec := triggerRequest(t, mux, `{"scope":"all"}{"scope":"products"}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if len(deps.queue.enqueued) != 0 {
		t.Error("expected no job enqueued for a rejected multi-value body")
	}
}

// TestSearchAdminHandler_Trigger_RejectedRequest_IsAudited pins a fixed
// gap: previously, a request rejected before reaching
// triggerBulk/triggerSingleProduct (malformed JSON, unknown scope, a bad
// since timestamp, etc.) produced no audit trail at all — only requests
// that passed every front-end check ever got audited. Trigger now audits
// every outcome, success or rejection, exactly once.
func TestSearchAdminHandler_Trigger_RejectedRequest_IsAudited(t *testing.T) {
	deps := newDefaultDeps()
	h, repo := newSearchAdminHandlerWithAuditRepo(t, deps)
	mux := newSearchAdminRouter(h)

	rec := triggerRequest(t, mux, `{"scope":"bogus"}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if len(repo.records) != 1 {
		t.Fatalf("audit records = %d, want exactly 1: %+v", len(repo.records), repo.records)
	}
	rec0 := repo.records[0]
	if rec0.Action != "search_reindex.trigger" || rec0.Result != "error" || rec0.ResourceType != "search_reindex" {
		t.Errorf("audit record = %+v, want action=search_reindex.trigger result=error resource_type=search_reindex", rec0)
	}
}

func TestSearchAdminHandler_Trigger_ScopeProducts_SingleID_NotFound(t *testing.T) {
	deps := newDefaultDeps()
	mux := newSearchAdminRouter(newSearchAdminHandler(t, deps))

	rec := triggerRequest(t, mux, `{"scope":"products","ids":["`+id.New()+`"]}`)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestSearchAdminHandler_Trigger_ScopeProducts_SingleID_InvalidUUID(t *testing.T) {
	deps := newDefaultDeps()
	mux := newSearchAdminRouter(newSearchAdminHandler(t, deps))

	rec := triggerRequest(t, mux, `{"scope":"products","ids":["not-a-uuid"]}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if len(deps.products.products) != 0 || len(deps.engine.indexed) != 0 {
		t.Error("expected no DB/engine calls for a malformed product id")
	}
}

// TestSearchAdminHandler_Trigger_ScopeProducts_MultipleIDs_InvalidUUID_Rejected
// pins a fixed bug: the multi-ID path used to skip id.IsValid entirely and
// let the malformed ID reach ReindexService.Trigger, whose plain
// fmt.Errorf validation error triggerBulk then force-wrapped as
// apperror.CodeInternal — a client input mistake surfacing as a generic
// 500 "internal server error" instead of 422. ReindexService's
// normalizeProductIDs now returns apperror.Validation, and triggerBulk
// forwards Trigger's error to JSONError unwrapped, so this must be 422.
func TestSearchAdminHandler_Trigger_ScopeProducts_MultipleIDs_InvalidUUID_Rejected(t *testing.T) {
	deps := newDefaultDeps()
	mux := newSearchAdminRouter(newSearchAdminHandler(t, deps))

	rec := triggerRequest(t, mux, `{"scope":"products","ids":["not-a-uuid","also-bad"]}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if len(deps.queue.enqueued) != 0 {
		t.Error("expected no job enqueued for a rejected malformed multi-ID request")
	}
}

// TestSearchAdminHandler_Trigger_ScopeProducts_TooManyIDs_Rejected pins the
// defense-in-depth cap (maxReindexRequestIDs) rejecting an oversized ids
// array before any per-ID validation work runs.
func TestSearchAdminHandler_Trigger_ScopeProducts_TooManyIDs_Rejected(t *testing.T) {
	deps := newDefaultDeps()
	mux := newSearchAdminRouter(newSearchAdminHandler(t, deps))

	ids := make([]string, 10_001)
	for i := range ids {
		ids[i] = id.New()
	}
	body, err := json.Marshal(map[string]interface{}{"scope": "products", "ids": ids})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	rec := triggerRequest(t, mux, string(body))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if len(deps.queue.enqueued) != 0 {
		t.Error("expected no job enqueued for a rejected oversized ids array")
	}
}

// TestSearchAdminHandler_Trigger_ScopeProducts_SingleID_ListByIDsError
// exercises triggerSingleProduct's DB-failure branch (distinct from the
// not-found and invalid-UUID cases already covered above).
func TestSearchAdminHandler_Trigger_ScopeProducts_SingleID_ListByIDsError(t *testing.T) {
	deps := newDefaultDeps()
	deps.products.listByIDsErr = errors.New("db down")
	mux := newSearchAdminRouter(newSearchAdminHandler(t, deps))

	rec := triggerRequest(t, mux, `{"scope":"products","ids":["`+id.New()+`"]}`)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	if len(deps.engine.indexed) != 0 {
		t.Error("expected no engine call when the product lookup itself fails")
	}
}

func TestSearchAdminHandler_Trigger_ScopeProducts_EmptyIDs(t *testing.T) {
	deps := newDefaultDeps()
	mux := newSearchAdminRouter(newSearchAdminHandler(t, deps))

	rec := triggerRequest(t, mux, `{"scope":"products","ids":[]}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

// TestSearchAdminHandler_Trigger_ScopeCategories_SingleID_StillEnqueues
// pins a deliberate deviation from PR-1035.md's original spec: a single
// category ID does NOT get the synchronous path, because that would
// require SearchEngine.IndexCategory, which doesn't exist yet (PR-1037 is
// still "planned"). See PR-1035.md's Round 1 notes.
func TestSearchAdminHandler_Trigger_ScopeCategories_SingleID_StillEnqueues(t *testing.T) {
	deps := newDefaultDeps()
	mux := newSearchAdminRouter(newSearchAdminHandler(t, deps))

	rec := triggerRequest(t, mux, `{"scope":"categories","ids":["cat-1"]}`)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if len(deps.queue.enqueued) != 1 {
		t.Fatalf("enqueued %d jobs, want 1", len(deps.queue.enqueued))
	}
}

func TestSearchAdminHandler_Trigger_ScopeCategories_EmptyIDs(t *testing.T) {
	deps := newDefaultDeps()
	mux := newSearchAdminRouter(newSearchAdminHandler(t, deps))

	rec := triggerRequest(t, mux, `{"scope":"categories","ids":[]}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestSearchAdminHandler_Trigger_ScopeSince_Valid_Enqueues202(t *testing.T) {
	deps := newDefaultDeps()
	mux := newSearchAdminRouter(newSearchAdminHandler(t, deps))

	past := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	rec := triggerRequest(t, mux, `{"scope":"since","since":"`+past+`"}`)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
}

func TestSearchAdminHandler_Trigger_ScopeSince_Future_Rejected(t *testing.T) {
	deps := newDefaultDeps()
	mux := newSearchAdminRouter(newSearchAdminHandler(t, deps))

	future := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	rec := triggerRequest(t, mux, `{"scope":"since","since":"`+future+`"}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	if len(deps.queue.enqueued) != 0 {
		t.Error("expected no job enqueued for a rejected future timestamp")
	}
}

func TestSearchAdminHandler_Trigger_ScopeSince_Malformed_Rejected(t *testing.T) {
	deps := newDefaultDeps()
	mux := newSearchAdminRouter(newSearchAdminHandler(t, deps))

	rec := triggerRequest(t, mux, `{"scope":"since","since":"not-a-timestamp"}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestSearchAdminHandler_Trigger_UnknownScope_Rejected(t *testing.T) {
	deps := newDefaultDeps()
	mux := newSearchAdminRouter(newSearchAdminHandler(t, deps))

	rec := triggerRequest(t, mux, `{"scope":"bogus"}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestSearchAdminHandler_Trigger_Forbidden(t *testing.T) {
	deps := newDefaultDeps()
	mux := newSearchAdminRouter(newSearchAdminHandler(t, deps))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/search/reindex", strings.NewReader(`{"scope":"all"}`))
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

func TestSearchAdminHandler_Get_ReturnsRunDetail(t *testing.T) {
	deps := newDefaultDeps()
	runID := id.New()
	started := time.Now().Add(-time.Minute).UTC()
	deps.runs.runs = map[string]*domainsearch.Run{
		runID: {
			ID: runID, Scope: "products", ScopeParams: map[string]interface{}{"requested_scope": "products"},
			Status: domainsearch.RunStatusProcessing, TotalCount: 10, ProcessedCount: 4, ErrorCount: 0,
			StartedAt: started,
		},
	}
	mux := newSearchAdminRouter(newSearchAdminHandler(t, deps))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/search/reindex/"+runID, nil)
	req.SetPathValue("runID", runID)
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
	if resp.Data["status"] != "processing" || resp.Data["total_count"].(float64) != 10 || resp.Data["processed_count"].(float64) != 4 {
		t.Errorf("data = %+v, want status=processing total_count=10 processed_count=4", resp.Data)
	}
	if _, present := resp.Data["finished_at"]; present {
		t.Errorf("finished_at present for a still-processing run: %+v", resp.Data)
	}
}

func TestSearchAdminHandler_Get_NotFound(t *testing.T) {
	deps := newDefaultDeps()
	mux := newSearchAdminRouter(newSearchAdminHandler(t, deps))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/search/reindex/missing", nil)
	req.SetPathValue("runID", "missing")
	req = testhelper.AdminRequest(req, "admin-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// TestSearchAdminHandler_Get_EmptyRunID mirrors
// TestJobAdminHandler_Get_EmptyID: net/http's {runID} pattern never
// actually dispatches an empty path segment, so this calls the handler
// func directly to exercise its own defense-in-depth check.
func TestSearchAdminHandler_Get_EmptyRunID(t *testing.T) {
	h := newSearchAdminHandler(t, newDefaultDeps())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/search/reindex/", nil)
	req.SetPathValue("runID", "")
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	h.Get()(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}
